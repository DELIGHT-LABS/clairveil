package keeper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const msgServerTestChainID = "clairveil-local-1"
const msgServerTestExpiry int64 = 4102444800

type mockPrivacyBankKeeper struct {
	fromAccountToModuleCalls int
	fromModuleToAccountCalls int
	lastAccountSender        sdk.AccAddress
	lastAccountModule        string
	lastAccountAmount        sdk.Coins
	errFromAccountToModule   error
	errFromModuleToAccount   error
	moduleBalances           sdk.Coins
}

var (
	depositArtifactOnce sync.Once
	depositArtifactErr  error
	depositTestR1CS     constraint.ConstraintSystem
	depositTestPK       groth16.ProvingKey
	batchTestR1CS       constraint.ConstraintSystem
	batchTestPK         groth16.ProvingKey
	joinSplitTestR1CS   constraint.ConstraintSystem
	joinSplitTestPK     groth16.ProvingKey
)

func (m *mockPrivacyBankKeeper) GetBalance(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, m.moduleBalances.AmountOf(denom))
}

func (m *mockPrivacyBankKeeper) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	m.fromAccountToModuleCalls++
	m.lastAccountSender = append(sdk.AccAddress(nil), sender...)
	m.lastAccountModule = module
	m.lastAccountAmount = append(sdk.Coins(nil), amt...)
	if m.errFromAccountToModule != nil {
		return m.errFromAccountToModule
	}
	m.moduleBalances = m.moduleBalances.Add(amt...)
	return nil
}

func (m *mockPrivacyBankKeeper) SendCoinsFromModuleToAccount(_ context.Context, _ string, _ sdk.AccAddress, amt sdk.Coins) error {
	m.fromModuleToAccountCalls++
	if m.errFromModuleToAccount != nil {
		return m.errFromModuleToAccount
	}
	m.moduleBalances = m.moduleBalances.Sub(amt...)
	return nil
}

func setupMsgServerKeeper() (*Keeper, sdk.Context, *mockPrivacyBankKeeper) {
	storeKey := storetypes.NewKVStoreKey(privacytypes.StoreKey)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)
	ctx = ctx.WithChainID(msgServerTestChainID)
	ctx = ctx.WithBlockTime(time.Unix(1700000000, 0))

	bankKeeper := &mockPrivacyBankKeeper{}
	k := NewKeeper(privacytypes.ModuleCdc, runtime.NewKVStoreService(storeKey), paramtypes.Subspace{}, bankKeeper)
	return k, ctx, bankKeeper
}

func setupRegisteredMsgServerKeeper(t testing.TB) (*Keeper, sdk.Context, *mockPrivacyBankKeeper) {
	t.Helper()
	k, ctx, bankKeeper := setupMsgServerKeeper()
	_, err := k.RegisterCanonicalAssetV1(ctx, "uclair")
	require.NoError(t, err)
	return k, ctx, bankKeeper
}

func fixedFieldBytes(v uint64) []byte {
	bz := make([]byte, fieldElementByteSize)
	binary.BigEndian.PutUint64(bz[fieldElementByteSize-8:], v)
	return bz
}

func testAddress(b byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{b}, 20)).String()
}

func ensureDepositTestArtifacts(t *testing.T) {
	t.Helper()

	depositArtifactOnce.Do(func() {
		dir, err := os.MkdirTemp("", "clairveil-keeper-zk-*")
		if err != nil {
			depositArtifactErr = err
			return
		}
		if err := os.Setenv(privacyzk.ZKArtifactDirEnv, dir); err != nil {
			depositArtifactErr = err
			return
		}

		depositCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.DepositCircuit{})
		if err != nil {
			depositArtifactErr = err
			return
		}
		depositPK, depositVK, err := groth16.Setup(depositCS)
		if err != nil {
			depositArtifactErr = err
			return
		}
		spendCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.SpendCircuit{})
		if err != nil {
			depositArtifactErr = err
			return
		}
		spendPK, spendVK, err := groth16.Setup(spendCS)
		if err != nil {
			depositArtifactErr = err
			return
		}
		joinSplitCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.JoinSplitCircuit{})
		if err != nil {
			depositArtifactErr = err
			return
		}
		joinSplitPK, joinSplitVK, err := groth16.Setup(joinSplitCS)
		if err != nil {
			depositArtifactErr = err
			return
		}
		batchCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.BatchJoinSplit16x32{})
		if err != nil {
			depositArtifactErr = err
			return
		}
		batchPK, batchVK, err := groth16.Setup(batchCS)
		if err != nil {
			depositArtifactErr = err
			return
		}

		depositTestR1CS = depositCS
		depositTestPK = depositPK
		batchTestR1CS = batchCS
		batchTestPK = batchPK
		joinSplitTestR1CS = joinSplitCS
		joinSplitTestPK = joinSplitPK
		depositArtifactErr = writeKeeperTestArtifacts(dir, []keeperTestArtifact{
			{privacyzk.DepositR1CSFile, depositCS},
			{privacyzk.DepositPKFile, depositPK},
			{privacyzk.DepositVKFile, depositVK},
			{privacyzk.SpendR1CSFile, spendCS},
			{privacyzk.SpendPKFile, spendPK},
			{privacyzk.SpendVKFile, spendVK},
			{privacyzk.JoinSplitR1CSFile, joinSplitCS},
			{privacyzk.JoinSplitPKFile, joinSplitPK},
			{privacyzk.JoinSplitVKFile, joinSplitVK},
			{privacyzk.BatchJoinSplit16x32R1CSFile, batchCS},
			{privacyzk.BatchJoinSplit16x32PKFile, batchPK},
			{privacyzk.BatchJoinSplit16x32VKFile, batchVK},
		})
	})
	require.NoError(t, depositArtifactErr)
}

type keeperTestArtifact struct {
	filename string
	object   interface {
		WriteTo(io.Writer) (int64, error)
	}
}

func writeKeeperTestArtifacts(dir string, artifacts []keeperTestArtifact) error {
	checksums := make(map[string]string, len(artifacts))
	descriptors := privacyzk.DefaultArtifactDescriptors()
	for _, artifact := range artifacts {
		file, err := os.Create(filepath.Join(dir, artifact.filename))
		if err != nil {
			return err
		}
		if _, err := artifact.object.WriteTo(file); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		artifactBytes, err := os.ReadFile(filepath.Join(dir, artifact.filename))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(artifactBytes)
		for _, descriptor := range descriptors {
			if descriptor.Filename == artifact.filename {
				checksums[descriptor.ChecksumEnv] = hex.EncodeToString(sum[:])
				break
			}
		}
	}
	manifestBytes, err := json.Marshal(privacyzk.ManifestFromChecksums(dir, "", checksums))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, privacyzk.ArtifactManifestFile), manifestBytes, 0o600)
}

type keeperDepositArtifactProvider struct{}

func (keeperDepositArtifactProvider) DepositR1CS() (constraint.ConstraintSystem, error) {
	return depositTestR1CS, nil
}

func (keeperDepositArtifactProvider) DepositProvingKey() (groth16.ProvingKey, error) {
	return depositTestPK, nil
}

type keeperDepositProofRunner struct{}

func (keeperDepositProofRunner) ProveDeposit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, depositWitness witness.Witness) (groth16.Proof, error) {
	return groth16.Prove(r1cs, provingKey, depositWitness)
}

func testDepositMsg(t *testing.T, creator, amountStr string, amount *big.Int, denom string, encryptedNote []byte) *privacytypes.MsgDeposit {
	t.Helper()
	ensureDepositTestArtifacts(t)

	spendPubKey := testKeeperScalarMulBase(big.NewInt(17))
	viewPubKey := testKeeperScalarMulBase(big.NewInt(19))
	spendX, spendY := testKeeperPointBigInts(spendPubKey)
	viewX, viewY := testKeeperPointBigInts(viewPubKey)
	note, err := privacytypes.NewNote(spendX, spendY, viewX, viewY, amount, denom, "test")
	require.NoError(t, err)

	proof, err := privacydeposit.BuildDepositProof(*note, keeperDepositArtifactProvider{}, keeperDepositProofRunner{})
	require.NoError(t, err)

	commitmentBytes := fixedFieldBytesFromBigInt(t, note.ComputeCommitment())
	if _, err := privacytypes.UnwrapEncryptedEnvelopeV1(encryptedNote, privacytypes.EnvelopeDepositNoteV1); err != nil {
		rawSize, sizeErr := privacytypes.EncryptedEnvelopeV1Size(privacytypes.EnvelopeDepositNoteV1)
		require.NoError(t, sizeErr)
		raw := make([]byte, rawSize-privacytypes.EncryptedEnvelopeV1HeaderSize)
		copy(raw, encryptedNote)
		encryptedNote, err = privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeDepositNoteV1, raw)
		require.NoError(t, err)
	}
	return privacytypes.NewMsgDeposit(creator, amountStr, commitmentBytes, encryptedNote, proof)
}

func testKeeperEnvelope(t *testing.T, kind privacytypes.EncryptedEnvelopeKindV1) []byte {
	t.Helper()
	total, err := privacytypes.EncryptedEnvelopeV1Size(kind)
	require.NoError(t, err)
	raw := make([]byte, total-privacytypes.EncryptedEnvelopeV1HeaderSize)
	wrapped, err := privacytypes.WrapEncryptedEnvelopeV1(kind, raw)
	require.NoError(t, err)
	return wrapped
}

func testKeeperDisclosurePubKey() []byte {
	point := testKeeperScalarMulBase(big.NewInt(71))
	encoded := point.Bytes()
	return append([]byte(nil), encoded[:]...)
}

func testStructurallyValidTransferMsg(
	t *testing.T,
	creator string,
	root []byte,
	nullifiers [][]byte,
	commitments [][]byte,
	expiresAtUnix int64,
) *privacytypes.MsgTransfer {
	t.Helper()
	return privacytypes.NewMsgTransferWithDisclosure(
		creator,
		[]byte{0x01},
		root,
		nullifiers,
		commitments,
		[][]byte{
			testKeeperEnvelope(t, privacytypes.EnvelopeTransferNoteV1),
			testKeeperEnvelope(t, privacytypes.EnvelopeTransferNoteV1),
		},
		[][]byte{{0x01, 0x02}, {0x03, 0x04}},
		privacytypes.TransferPrivacyPolicyAllPrivate,
		nil,
		privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
		nil,
		nil,
		fixedFieldBytes(61),
		testKeeperDisclosurePubKey(),
		testKeeperEnvelope(t, privacytypes.EnvelopeAuditDisclosureV1),
		nil,
		nil,
		expiresAtUnix,
	)
}

func testKeeperScalarMulBase(scalar *big.Int) crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var base crypto_tedwards.PointAffine
	base.X.Set(&curve.Base.X)
	base.Y.Set(&curve.Base.Y)
	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&base, scalar)
	return pubKey
}

func testKeeperPointBigInts(point crypto_tedwards.PointAffine) (*big.Int, *big.Int) {
	x := new(big.Int)
	y := new(big.Int)
	point.X.BigInt(x)
	point.Y.BigInt(y)
	return x, y
}

func fixedFieldBytesFromBigInt(t *testing.T, value *big.Int) []byte {
	t.Helper()
	bz := value.Bytes()
	require.LessOrEqual(t, len(bz), fieldElementByteSize)
	out := make([]byte, fieldElementByteSize)
	copy(out[fieldElementByteSize-len(bz):], bz)
	return out
}

func TestMsgServerDepositSuccess(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	msg := testDepositMsg(t, testAddress(0x11), "1uclair", big.NewInt(1), "uclair", []byte{0x01})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)
	require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, uint64(1), k.GetLeafCount(ctx))

	snapshot, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.Equal(t, "uclair", snapshot.Denom)
	require.Equal(t, "1", snapshot.ModuleBalance.String())
	require.Equal(t, "1", snapshot.TotalDeposited.String())
	require.Equal(t, "0", snapshot.TotalWithdrawn.String())
	require.Equal(t, "1", snapshot.ExpectedModuleBalance.String())
	require.True(t, snapshot.InvariantHolds)

	root := k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)
	require.NotEmpty(t, root)
	require.True(t, k.CheckHistoricalRoot(ctx, root))
}

func TestMsgServerDepositProofFramingAndGasFailurePathsKeepStateUnchanged(t *testing.T) {
	ensureDepositTestArtifacts(t)
	consumed := make(map[string]storetypes.Gas)
	for _, tc := range []struct {
		name  string
		proof func(*testing.T) []byte
	}{
		{name: "short framing", proof: func(*testing.T) []byte { return []byte{0x01} }},
		{name: "framing valid invalid proof", proof: canonicalTestProofBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
			ctx = ctx.WithGasMeter(storetypes.NewGasMeter(2 * DepositProofVerificationGas))
			msg := privacytypes.NewMsgDeposit(testAddress(0x13), "1uclair", fixedFieldBytes(0x7d), testKeeperEnvelope(t, privacytypes.EnvelopeDepositNoteV1), tc.proof(t))

			_, err := NewMsgServerImpl(*k).Deposit(sdk.WrapSDKContext(ctx), msg)
			require.Error(t, err)
			consumed[tc.name] = ctx.GasMeter().GasConsumed()
			require.Equal(t, uint64(0), k.GetLeafCount(ctx))
			require.Equal(t, 0, bankKeeper.fromAccountToModuleCalls)
		})
	}
	require.Equal(t, storetypes.Gas(DepositProofVerificationGas), consumed["framing valid invalid proof"]-consumed["short framing"])
}

func TestMsgServerDepositRejectsGlobalCommitmentCollisionBeforeBank(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)
	msg := testDepositMsg(t, testAddress(0x12), "1uclair", big.NewInt(1), "uclair", []byte{0x01})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)
	rootBefore := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	countBefore := k.GetLeafCount(ctx)
	bankCallsBefore := bankKeeper.fromAccountToModuleCalls

	_, err = server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.ErrorContains(t, err, "note commitment already exists")
	require.Equal(t, bankCallsBefore, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, countBefore, k.GetLeafCount(ctx))
	require.Equal(t, rootBefore, k.GetMerkleNode(ctx, uint8(MerkleDepth), 0))
}

func TestMsgServerDepositEmitsExpectedEvent(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	msg := testDepositMsg(t, testAddress(0x13), "1uclair", big.NewInt(1), "uclair", []byte{0xaa, 0xbb})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)

	var depositEvent sdk.Event
	found := false
	for _, event := range ctx.EventManager().Events() {
		if event.Type == privacytypes.EventTypeDeposit {
			depositEvent = event
			found = true
			break
		}
	}
	require.True(t, found)

	creatorAttr, ok := depositEvent.GetAttribute(privacytypes.AttributeKeyCreator)
	require.True(t, ok)
	require.Equal(t, msg.Creator, creatorAttr.Value)

	commitmentAttr, ok := depositEvent.GetAttribute(privacytypes.AttributeKeyCommitment)
	require.True(t, ok)
	require.Equal(t, fmt.Sprintf("%x", msg.NoteCommitment), commitmentAttr.Value)

	encryptedAttr, ok := depositEvent.GetAttribute(privacytypes.AttributeKeyEncryptedNote)
	require.True(t, ok)
	require.Equal(t, fmt.Sprintf("%x", msg.EncryptedNote), encryptedAttr.Value)
}

func TestMsgServerDepositRejectsProofTamperingBeforeBank(t *testing.T) {
	tests := []struct {
		name          string
		mutateMsg     func(t *testing.T, msg *privacytypes.MsgDeposit)
		emptyReserves []string
	}{
		{
			name:          "amount",
			emptyReserves: []string{"uclair"},
			mutateMsg: func(_ *testing.T, msg *privacytypes.MsgDeposit) {
				msg.Amount = "2uclair"
			},
		},
		{
			name:          "denom",
			emptyReserves: []string{"uclair", "uatom"},
			mutateMsg: func(_ *testing.T, msg *privacytypes.MsgDeposit) {
				msg.Amount = "1uatom"
			},
		},
		{
			name:          "commitment",
			emptyReserves: []string{"uclair"},
			mutateMsg: func(_ *testing.T, msg *privacytypes.MsgDeposit) {
				msg.NoteCommitment = fixedFieldBytes(99)
			},
		},
		{
			name:          "proof",
			emptyReserves: []string{"uclair"},
			mutateMsg: func(t *testing.T, msg *privacytypes.MsgDeposit) {
				other := testDepositMsg(t, msg.Creator, "2uclair", big.NewInt(2), "uclair", []byte{0x02})
				msg.Proof = other.Proof
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
			if tc.name == "denom" {
				_, err := k.RegisterCanonicalAssetV1(ctx, "uatom")
				require.NoError(t, err)
			}
			server := NewMsgServerImpl(*k)
			msg := testDepositMsg(t, testAddress(0x21), "1uclair", big.NewInt(1), "uclair", []byte{0x01})
			tc.mutateMsg(t, msg)

			_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
			require.Error(t, err)
			require.Contains(t, err.Error(), "deposit proof verification failed")
			require.Equal(t, 0, bankKeeper.fromAccountToModuleCalls)
			require.Equal(t, uint64(0), k.GetLeafCount(ctx))

			for _, denom := range tc.emptyReserves {
				snapshot, snapshotErr := k.GetReserveSnapshot(ctx, denom)
				require.NoError(t, snapshotErr)
				require.Equal(t, "0", snapshot.ModuleBalance.String(), denom)
				require.Equal(t, "0", snapshot.TotalDeposited.String(), denom)
				require.Equal(t, "0", snapshot.TotalWithdrawn.String(), denom)
			}
		})
	}
}

func TestMsgServerDepositRejectsInvalidCommitmentBeforeBank(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	msg := privacytypes.NewMsgDeposit(testAddress(0x22), "1uclair", []byte{0x01}, []byte{0x01}, []byte{0x01})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "note commitment must be exactly 32 bytes")
	require.Equal(t, 0, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, uint64(0), k.GetLeafCount(ctx))
}

func TestMsgServerDepositRejectsFullTreeBeforeBank(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)
	k.SetLeafCount(ctx, MaxMerkleLeaves)

	msg := privacytypes.NewMsgDeposit(testAddress(0x24), "1uclair", fixedFieldBytes(3), testKeeperEnvelope(t, privacytypes.EnvelopeDepositNoteV1), []byte{0x01})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not enough merkle tree capacity for deposit output")
	require.Equal(t, 0, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, MaxMerkleLeaves, k.GetLeafCount(ctx))
}

func TestMsgServerDepositRejectsUnsafeMissingRootBeforeBank(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)
	k.SetLeafCount(ctx, MaxMerkleRebuildLeaves+1)

	msg := privacytypes.NewMsgDeposit(testAddress(0x25), "1uclair", fixedFieldBytes(4), testKeeperEnvelope(t, privacytypes.EnvelopeDepositNoteV1), []byte{0x01})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cached root is required")
	require.NotContains(t, err.Error(), "not enough merkle tree capacity")
	require.Equal(t, 0, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, MaxMerkleRebuildLeaves+1, k.GetLeafCount(ctx))
}

func TestMsgServerWithdrawRejectsRootNotFoundBeforeZK(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x33),
		[]byte{0x01},
		fixedFieldBytes(10),
		fixedFieldBytes(11),
		"1uclair",
		testAddress(0x44),
		msgServerTestChainID,
		msgServerTestExpiry,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "withdraw root was not found in the historical merkle roots")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
}

func TestMsgServerWithdrawRejectsUsedNullifierBeforeZK(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(20)
	nullifier := fixedFieldBytes(21)
	k.SetHistoricalRoot(ctx, root)
	k.SetNullifier(ctx, nullifier)

	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x55),
		[]byte{0x01},
		root,
		nullifier,
		"1uclair",
		testAddress(0x66),
		msgServerTestChainID,
		msgServerTestExpiry,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "withdraw nullifier was already used")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
}

func TestMsgServerWithdrawRejectsInvalidRecipientBeforeZK(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(23)
	nullifier := fixedFieldBytes(24)
	k.SetHistoricalRoot(ctx, root)

	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x57),
		[]byte{0x01},
		root,
		nullifier,
		"1uclair",
		"invalid-recipient",
		msgServerTestChainID,
		msgServerTestExpiry,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "recipient address")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
	require.False(t, k.HasNullifier(ctx, nullifier))
}

func TestMsgServerWithdrawRejectsChainIDMismatch(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(26)
	nullifier := fixedFieldBytes(27)
	k.SetHistoricalRoot(ctx, root)

	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x59),
		[]byte{0x01},
		root,
		nullifier,
		"1uclair",
		testAddress(0x6a),
		"wrong-chain",
		msgServerTestExpiry,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "chain id mismatch")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
	require.False(t, k.HasNullifier(ctx, nullifier))
}

func TestMsgServerWithdrawRejectsExpiredPayload(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(29)
	nullifier := fixedFieldBytes(30)
	k.SetHistoricalRoot(ctx, root)

	expiredAt := ctx.BlockTime().Add(-time.Second).Unix()
	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x5b),
		[]byte{0x01},
		root,
		nullifier,
		"1uclair",
		testAddress(0x6c),
		msgServerTestChainID,
		expiredAt,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "withdraw payload has expired")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
	require.False(t, k.HasNullifier(ctx, nullifier))
}

func TestMsgServerWithdrawTreatsExpiryBoundaryAsExpired(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)
	root := fixedFieldBytes(0x7b)
	nullifier := fixedFieldBytes(0x7c)
	k.SetHistoricalRoot(ctx, root)

	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x5c),
		[]byte{0x01},
		root,
		nullifier,
		"1uclair",
		testAddress(0x6d),
		msgServerTestChainID,
		ctx.BlockTime().Unix(),
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.ErrorContains(t, err, "withdraw payload has expired")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
	require.False(t, k.HasNullifier(ctx, nullifier))
}

func TestMsgServerTransferRejectsRootNotFoundBeforeZK(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	msg := testStructurallyValidTransferMsg(
		t,
		testAddress(0x77),
		fixedFieldBytes(30),
		[][]byte{fixedFieldBytes(31), fixedFieldBytes(32)},
		[][]byte{fixedFieldBytes(33), fixedFieldBytes(34)},
		ctx.BlockTime().Add(time.Hour).Unix(),
	)

	_, err := server.Transfer(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "transfer root was not found in the historical merkle roots")
}

func TestMsgServerTransferTreatsExpiryBoundaryAsExpired(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)
	msg := testStructurallyValidTransferMsg(
		t,
		testAddress(0x78),
		fixedFieldBytes(30),
		[][]byte{fixedFieldBytes(31), fixedFieldBytes(32)},
		[][]byte{fixedFieldBytes(33), fixedFieldBytes(34)},
		ctx.BlockTime().Unix(),
	)

	_, err := server.Transfer(sdk.WrapSDKContext(ctx), msg)
	require.ErrorContains(t, err, "transfer payload has expired")
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(31)))
	require.Equal(t, uint64(0), k.GetLeafCount(ctx))
}

func TestMsgServerTransferRejectsInvalidNullifierCountBeforeZK(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(40)
	k.SetHistoricalRoot(ctx, root)

	msg := privacytypes.NewMsgTransfer(
		testAddress(0x88),
		[]byte{0x01},
		root,
		[][]byte{fixedFieldBytes(41)},
		[][]byte{fixedFieldBytes(42), fixedFieldBytes(43)},
		[][]byte{{0x01}, {0x02}},
		[][]byte{{0x01, 0x02}, {0x03, 0x04}},
		ctx.BlockTime().Add(time.Hour).Unix(),
	)

	_, err := server.Transfer(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "transfer requires exactly 2 nullifiers")
}

func TestMsgServerTransferRejectsLocalDuplicatesBeforeProof(t *testing.T) {
	for _, tc := range []struct {
		name        string
		nullifiers  [][]byte
		commitments [][]byte
		want        string
	}{
		{
			name:        "nullifier",
			nullifiers:  [][]byte{fixedFieldBytes(44), fixedFieldBytes(44)},
			commitments: [][]byte{fixedFieldBytes(45), fixedFieldBytes(46)},
			want:        "nullifier index 1 duplicates index 0",
		},
		{
			name:        "commitment",
			nullifiers:  [][]byte{fixedFieldBytes(47), fixedFieldBytes(48)},
			commitments: [][]byte{fixedFieldBytes(49), fixedFieldBytes(49)},
			want:        "commitment index 1 duplicates index 0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, ctx, _ := setupRegisteredMsgServerKeeper(t)
			root := fixedFieldBytes(43)
			k.SetHistoricalRoot(ctx, root)

			err := msgServer{Keeper: *k}.executeShieldedTransfer(ctx, shieldedTransferRequest{
				expiresAtUnix:  4_102_444_800,
				root:           root,
				proof:          []byte{0x01},
				nullifiers:     tc.nullifiers,
				newCommitments: tc.commitments,
				cipherTexts:    [][]byte{{0x01}, {0x02}},
				viewTags:       [][]byte{{0x01, 0x02}, {0x03, 0x04}},
			})
			require.ErrorContains(t, err, tc.want)
			for _, nullifier := range tc.nullifiers {
				require.False(t, k.HasNullifier(ctx, nullifier))
			}
			require.Equal(t, uint64(0), k.GetLeafCount(ctx))
		})
	}
}

func TestMsgServerTransferRejectsGlobalCommitmentCollisionBeforeProof(t *testing.T) {
	k, ctx := setupTreeKeeper()
	existing := fixedFieldBytes(56)
	require.NoError(t, k.AppendCommitment(ctx, existing))
	root := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	auditPubKey := testKeeperDisclosurePubKey()
	k.SetAuditMasterPubkey(ctx, auditPubKey)
	countBefore := k.GetLeafCount(ctx)
	rootBefore := append([]byte(nil), root...)

	err := msgServer{Keeper: *k}.executeShieldedTransfer(ctx, shieldedTransferRequest{
		expiresAtUnix:               4_102_444_800,
		root:                        root,
		proof:                       []byte{0x01},
		nullifiers:                  [][]byte{fixedFieldBytes(58), fixedFieldBytes(59)},
		newCommitments:              [][]byte{existing, fixedFieldBytes(60)},
		cipherTexts:                 [][]byte{{0x01}, {0x02}},
		viewTags:                    [][]byte{{0x01, 0x02}, {0x03, 0x04}},
		auditDisclosureTargetPubKey: auditPubKey,
	})
	require.ErrorContains(t, err, "commitment 0 already exists")
	require.Equal(t, countBefore, k.GetLeafCount(ctx))
	require.Equal(t, rootBefore, k.GetMerkleNode(ctx, uint8(MerkleDepth), 0))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(58)))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(59)))
}

func TestMsgServerTransferRejectsInsufficientBatchCapacityBeforeProof(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	k.SetLeafCount(ctx, MaxMerkleLeaves-1)

	root := fixedFieldBytes(50)
	auditPubKey := testKeeperDisclosurePubKey()
	k.SetHistoricalRoot(ctx, root)
	k.SetAuditMasterPubkey(ctx, auditPubKey)

	err := msgServer{Keeper: *k}.executeShieldedTransfer(ctx, shieldedTransferRequest{
		expiresAtUnix:               4_102_444_800,
		relayer:                     testAddress(0x89),
		proof:                       []byte{0x01},
		root:                        root,
		nullifiers:                  [][]byte{fixedFieldBytes(52), fixedFieldBytes(53)},
		newCommitments:              [][]byte{fixedFieldBytes(54), fixedFieldBytes(55)},
		cipherTexts:                 [][]byte{{0x01}, {0x02}},
		viewTags:                    [][]byte{{0x01, 0x02}, {0x03, 0x04}},
		auditDisclosureTargetPubKey: auditPubKey,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not enough merkle tree capacity for transfer outputs")
	require.Equal(t, MaxMerkleLeaves-1, k.GetLeafCount(ctx))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(52)))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(53)))
}

func TestMsgServerTransferRejectsOverflowAsTreeStateError(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	k.SetLeafCount(ctx, MaxMerkleLeaves+1)

	root := fixedFieldBytes(60)
	auditPubKey := testKeeperDisclosurePubKey()
	k.SetHistoricalRoot(ctx, root)
	k.SetAuditMasterPubkey(ctx, auditPubKey)

	err := msgServer{Keeper: *k}.executeShieldedTransfer(ctx, shieldedTransferRequest{
		expiresAtUnix:               4_102_444_800,
		relayer:                     testAddress(0x8a),
		proof:                       []byte{0x01},
		root:                        root,
		nullifiers:                  [][]byte{fixedFieldBytes(62), fixedFieldBytes(63)},
		newCommitments:              [][]byte{fixedFieldBytes(64), fixedFieldBytes(65)},
		cipherTexts:                 [][]byte{{0x01}, {0x02}},
		viewTags:                    [][]byte{{0x01, 0x02}, {0x03, 0x04}},
		auditDisclosureTargetPubKey: auditPubKey,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max capacity")
	require.NotContains(t, err.Error(), "not enough merkle tree capacity")
	require.Equal(t, MaxMerkleLeaves+1, k.GetLeafCount(ctx))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(62)))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(63)))
}
