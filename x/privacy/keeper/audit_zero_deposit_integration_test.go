package keeper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/proverservice"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	edwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

// This uses a freshly generated bundle, SDK preparation and the HTTP prover;
// a checked-in proof would silently bind the test to an obsolete setup key.
func TestAuditZeroDepositRealProofIntegration(t *testing.T) {
	dir := os.Getenv("CLAIRVEIL_ZERO_DEPOSIT_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set CLAIRVEIL_ZERO_DEPOSIT_ARTIFACT_DIR to a fresh development bundle")
	}
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{ArtifactDir: dir, CircuitSetID: privacyzk.AuditFieldCircuitSetID, RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment})
	require.NoError(t, err)
	identity, err := registry.LocalCircuitSetIdentity()
	require.NoError(t, err)
	manifest, err := os.ReadFile(filepath.Join(dir, privacyzk.ArtifactManifestFile))
	require.NoError(t, err)
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(big.NewInt(23).FillBytes(make([]byte, 32)))
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	require.NoError(t, err)
	actor := sdk.AccAddress(bytes.Repeat([]byte{0x31}, 20))
	funder := sdk.AccAddress(bytes.Repeat([]byte{0x32}, 20))
	newState := func(t *testing.T) (*Keeper, sdk.Context, *cacheAwareDepositBankKeeper, privacyaudit.Snapshot) {
		t.Helper()
		k, ctx, bank := setupCacheAwareDepositKeeper(t)
		ctx = ctx.WithBlockTime(time.Now().UTC().Truncate(time.Second))
		require.NoError(t, k.ConfigureAuditRuntime(AuditRuntimeConfig{PrincipalAdapter: auditApplyTestPrincipal{bank: bank}, ArtifactDir: dir, ExpectedIdentity: identity, NetworkNonce: [32]byte{0x49}, InitialHeight: 1, Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String()}))
		network, err := k.auditNetwork(ctx)
		require.NoError(t, err)
		pop, err := privacycrypto.CreateAuditPoP96(secret, network, 1, 0)
		require.NoError(t, err)
		popBytes, err := pop.Bytes()
		require.NoError(t, err)
		record := &auditKeyRecord{Epoch: 1, KeyID: key.ID(), Suite: 2, PK: key.Point().Bytes(), PoP: popBytes, Origin: auditOrigin{Kind: 3}}
		encoded, err := encodeAuditKey(record, network, 1)
		require.NoError(t, err)
		store := k.storeService.OpenKVStore(ctx)
		require.NoError(t, store.Set(auditEpochKey(auditEpochPrefix, 1), encoded))
		active := make([]byte, 18)
		binary.BigEndian.PutUint16(active, 1)
		binary.BigEndian.PutUint64(active[2:10], 1)
		require.NoError(t, store.Set([]byte{auditActivePrefix}, active))
		bank.setTestBalance(t, ctx, actor, "uclair", 50)
		bank.setTestBalance(t, ctx, funder, "uclair", 30)
		return k, ctx, bank, privacyaudit.Snapshot{Network: network, Epoch: 1, Key: key, StateHeight: ctx.BlockHeight(), ArtifactHash: sha256.Sum256(manifest)}
	}
	_, ctx, _, snapshot := newState(t)
	curve := edwards.GetEdwardsCurve()
	var spend, view edwards.PointAffine
	spend.ScalarMultiplication(&curve.Base, big.NewInt(11))
	view.ScalarMultiplication(&curve.Base, big.NewInt(13))
	sx, sy, err := privacycrypto.PublicPointFieldValues(spend)
	require.NoError(t, err)
	vx, vy, err := privacycrypto.PublicPointFieldValues(view)
	require.NoError(t, err)
	note, err := privacytypes.NewSecretNoteV1(sx, sy, vx, vy, 0, privacytypes.ComputeSecretAssetIDV1("uclair"), privacycrypto.FieldValueFromUint64(17), "")
	require.NoError(t, err)
	commitment, err := note.CommitmentV1()
	require.NoError(t, err)
	raw := commitment.Bytes()
	size, err := privacytypes.EncryptedEnvelopeV1Size(privacytypes.EnvelopeDepositNoteV1)
	require.NoError(t, err)
	cipher, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeDepositNoteV1, make([]byte, size-privacytypes.EncryptedEnvelopeV1HeaderSize))
	require.NoError(t, err)
	prepared, err := privacydeposit.PrepareAuditV2FromNormalNote(snapshot, actor.String(), "0uclair", *note, &privacyv2.OutputEffect{Commitment: raw[:], Ciphertext: cipher}, ctx.BlockTime().Add(time.Hour).Unix())
	require.NoError(t, err)
	defer prepared.Clear()
	full, err := privacydeposit.BuildAuditV2Witness(prepared, *note)
	require.NoError(t, err)
	handler := proverservice.NewAuditFieldHandler(registry, identity, func() time.Time { return ctx.BlockTime() }, io.Discard, proverservice.DefaultMaxRequestBz, "")
	server := httptest.NewServer(handler)
	defer server.Close()
	message, err := provertransport.ProvePreparedAuditField(context.Background(), provertransport.HTTPProverClient{BaseURL: server.URL, Client: server.Client()}, prepared, full, func(context.Context) (privacyaudit.Snapshot, error) { return snapshot, nil }, registry, identity)
	require.NoError(t, err)
	deposit := message.(*privacyv2.MsgDeposit)
	t.Run("funder endpoints", func(t *testing.T) {
		for _, endpoint := range []sdk.AccAddress{nil, authtypes.NewModuleAddress(privacytypes.ModuleName), authtypes.NewModuleAddress(govtypes.ModuleName)} {
			k, ctx, bank, _ := newState(t)
			before := collectStoreEntries(t, k.storeService, ctx)
			bankBefore := collectStoreEntries(t, bank.storeService, ctx)
			_, err := k.DepositWithFunderV2(ctx, deposit, endpoint)
			require.Error(t, err)
			require.Zero(t, bank.fromAccountToModuleCalls)
			require.Equal(t, before, collectStoreEntries(t, k.storeService, ctx))
			require.Equal(t, bankBefore, collectStoreEntries(t, bank.storeService, ctx))
			require.Empty(t, ctx.EventManager().Events())
		}
	})
	for _, delegated := range []bool{false, true} {
		name := "native"
		if delegated {
			name = "delegated"
		}
		t.Run(name, func(t *testing.T) {
			execute := func(k *Keeper, ctx sdk.Context, m *privacyv2.MsgDeposit) error {
				if delegated {
					_, err := k.DepositWithFunderV2(ctx, m, funder)
					return err
				}
				_, err := NewAuditMsgServerImpl(*k).Deposit(ctx, m)
				return err
			}
			for _, failure := range []string{"none", "amount", "proof", "envelope", "reserve", "tree", "scan"} {
				t.Run(failure, func(t *testing.T) {
					k, ctx, bank, _ := newState(t)
					m := proto.Clone(deposit).(*privacyv2.MsgDeposit)
					if failure == "amount" {
						m.Amount = "1uclair"
					}
					if failure == "proof" {
						m.Proof[0] ^= 1
					}
					if failure == "envelope" {
						m.Audit.Envelope[len(m.Audit.Envelope)-1] ^= 1
					}
					if failure == "reserve" {
						require.NoError(t, k.storeService.OpenKVStore(ctx).Set(privacytypes.GetReserveDepositKey("uclair"), []byte("invalid")))
					}
					if failure == "tree" {
						root := naiveRootFromLeaves([][]byte{m.Output.Commitment})
						require.NoError(t, k.storeService.OpenKVStore(ctx).Set(privacytypes.GetMerkleRootSnapshotKey(root), []byte{0xff}))
					}
					if failure == "scan" {
						require.NoError(t, k.storeService.OpenKVStore(ctx).Set(privacytypes.GetPrivacyScanSequenceKey(1), sdk.Uint64ToBigEndian(uint64(ctx.BlockHeight()))))
					}
					before := collectStoreEntries(t, k.storeService, ctx)
					bankBefore := collectStoreEntries(t, bank.storeService, ctx)
					meter := &zeroDepositGasMeter{GasMeter: storetypes.NewInfiniteGasMeter()}
					ctx = ctx.WithGasMeter(meter)
					err := execute(k, ctx, m)
					require.Zero(t, bank.fromAccountToModuleCalls)
					require.Equal(t, bankBefore, collectStoreEntries(t, bank.storeService, ctx))
					if failure != "none" {
						require.Error(t, err)
						if failure == "scan" {
							require.ErrorContains(t, err, "already indexed")
						}
						require.Equal(t, before, collectStoreEntries(t, k.storeService, ctx))
						require.Empty(t, ctx.EventManager().Events())
						return
					}
					require.NoError(t, err)
					var eventBytes uint64
					if delegated {
						payload, marshalErr := m.Marshal()
						require.NoError(t, marshalErr)
						eventBytes = (&delegatedDepositExecution{funder: funder, payload: payload}).eventBytes()
					}
					expectedMeter := storetypes.NewInfiniteGasMeter()
					_, gasErr := prechargeAuditMessage(ctx.WithGasMeter(expectedMeter), m, eventBytes)
					require.NoError(t, gasErr)
					require.Positive(t, meter.precharge)
					require.Equal(t, expectedMeter.GasConsumed(), meter.precharge)
					positive := proto.Clone(m).(*privacyv2.MsgDeposit)
					positive.Amount = "1uclair"
					positiveMeter := storetypes.NewInfiniteGasMeter()
					_, gasErr = prechargeAuditMessage(ctx.WithGasMeter(positiveMeter), positive, eventBytes)
					require.NoError(t, gasErr)
					require.Equal(t, positiveMeter.GasConsumed(), meter.precharge)
					require.EqualValues(t, 1, k.GetLeafCount(ctx))
					found, err := k.HasCommitment(ctx, m.Output.Commitment)
					require.NoError(t, err)
					require.True(t, found)
					seq, err := k.GetPrivacyGlobalSequence(ctx)
					require.NoError(t, err)
					require.EqualValues(t, 1, seq)
					scan, err := k.GetPrivacyScanPageV2(ctx, nil, 1, 1, MaxPrivacyScanByteLimit, nil)
					require.NoError(t, err)
					require.Len(t, scan.Outputs, 1)
					require.Equal(t, m.Output.Commitment, scan.Outputs[0].Commitment)
					reserve, err := k.GetReserveSnapshot(ctx, "uclair")
					require.NoError(t, err)
					require.True(t, reserve.TotalDeposited.IsZero())
				})
			}
		})
	}
}

// Record the actual route's explicit precharge separately from store gas.
type zeroDepositGasMeter struct {
	storetypes.GasMeter
	precharge uint64
}

func (m *zeroDepositGasMeter) ConsumeGas(amount storetypes.Gas, descriptor string) {
	if descriptor == "audit transition precharge" {
		m.precharge += amount
	}
	m.GasMeter.ConsumeGas(amount, descriptor)
}

func TestAuditZeroDepositNativeModuleEndpointRejected(t *testing.T) {
	fixture := newAuditApplyBoundaryFixture(t, auditfield.KindDeposit)
	message := fixture.msg.(*privacyv2.MsgDeposit)
	message.Amount = "0uclair"
	message.Creator = authtypes.NewModuleAddress(privacytypes.ModuleName).String()
	validated, err := privacytypes.ValidateAuditMessage(message)
	require.NoError(t, err)
	fixture.validated = validated
	fixture.record.Transparent.From = authtypes.NewModuleAddress(privacytypes.ModuleName)
	fixture.record.Transparent.Amount = auditfield.Field32FromUint64(0)
	before := snapshotAuditApplyBoundary(t, fixture)
	_, err = fixture.apply()
	require.ErrorContains(t, err, "invalid principal endpoint")
	require.Zero(t, fixture.bank.fromAccountToModuleCalls)
	require.Equal(t, before, snapshotAuditApplyBoundary(t, fixture))
}
