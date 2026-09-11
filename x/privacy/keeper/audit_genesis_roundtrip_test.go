package keeper

import (
	"encoding/binary"
	"math/big"
	"strings"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/internal/auditinit"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestAuditGenesisExportImportRetainsKeyManagementAndPrivacyState(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithChainID("audit-genesis-roundtrip-1").WithBlockHeight(10)
	identity := auditGenesisTestIdentity()
	k.audit = &auditRuntime{identity: identity, initialHeight: 1}
	anchor := [32]byte{9}

	keys := make([]*auditKeyRecord, 0, 4)
	for epoch, scalar := range []int64{31, 37, 41, 43} {
		activation, registered := uint64(0), uint64(0)
		origin := auditOrigin{Kind: 3, Anchor: anchor}
		if epoch > 0 {
			activation = []uint64{5, 12, 20}[epoch-1]
			registered = []uint64{3, 6, 7}[epoch-1]
			origin = auditOrigin{Kind: 1, Anchor: [32]byte{byte(epoch)}, Height: registered}
		}
		keys = append(keys, auditGenesisTestKey(t, k, ctx, scalar, uint64(epoch+1), activation, registered, origin))
	}

	require.NoError(t, auditinit.Run(ctx, func(init sdk.Context) error {
		if err := k.SetCircuitSetIdentity(init, identity); err != nil {
			return err
		}
		if err := k.InitGenesisAssetRegistryV1(init, types.DefaultGenesis(identity).AssetRegistry); err != nil {
			return err
		}
		if err := k.InitGenesisNullifiers(init, [][]byte{fixedFieldBytes(7)}); err != nil {
			return err
		}
		if err := k.InitGenesisReserveBalancesV1(init, []*types.ReserveBalanceV1{{CanonicalDenom: "uclair", TotalDeposited: "9", TotalWithdrawn: "0"}}); err != nil {
			return err
		}
		event := &types.PrivacyEventRecordV1{Height: 9, GlobalSequence: 1, EventType: types.EventTypeWithdraw}
		summary := &types.PrivacyScanSummaryV2{GlobalSequence: 1, Height: 9, EventType: types.EventTypeWithdraw, Nullifiers: [][]byte{fixedFieldBytes(7)}, CircuitSetId: types.ActiveCircuitSetID, PayloadVersion: types.FixedPayloadVersionV1, ScanSchemaVersion: types.PrivacyScanSchemaVersionV2}
		return k.InitGenesisPrivacyIndexV2(init, 1, []*types.PrivacyEventRecordV1{event}, []*types.PrivacyScanSummaryV2{summary}, nil)
	}))

	network, err := k.auditNetwork(ctx)
	require.NoError(t, err)
	store := k.storeService.OpenKVStore(ctx)
	require.NoError(t, store.Set([]byte{0x1b}, anchor[:]))
	for _, key := range keys {
		raw, err := encodeAuditKey(key, network, 1)
		require.NoError(t, err)
		require.NoError(t, store.Set(auditEpochKey(auditEpochPrefix, key.Epoch), raw))
	}
	active := make([]byte, 18)
	binary.BigEndian.PutUint16(active, 1)
	binary.BigEndian.PutUint64(active[2:], 2)
	binary.BigEndian.PutUint64(active[10:], 5)
	require.NoError(t, store.Set([]byte{auditActivePrefix}, active))
	pending := make([]byte, 8)
	binary.BigEndian.PutUint64(pending, 4)
	require.NoError(t, store.Set(auditEpochKey(auditActivationPrefix, 20), pending))
	cancelOrigin := auditOrigin{Kind: 1, Anchor: [32]byte{3}, Height: 6}
	cancel, err := encodeAuditCancellation(cancelOrigin, 6)
	require.NoError(t, err)
	require.NoError(t, store.Set(auditEpochKey(auditCancellationPrefix, 3), cancel))
	require.NoError(t, store.Set([]byte{auditHaltPrefix}, []byte{1}))

	metadata, err := k.ExportAuditGenesisMetadata(ctx)
	require.NoError(t, err)
	state := types.DefaultGenesis(identity)
	state.Nullifiers, err = k.ExportGenesisNullifiers(ctx)
	require.NoError(t, err)
	state.AssetRegistry, err = k.ExportGenesisAssetRegistryV1(ctx)
	require.NoError(t, err)
	state.ReserveBalances, err = k.ExportGenesisReserveBalancesV1(ctx)
	require.NoError(t, err)
	state.PrivacyEvents, err = k.ExportGenesisPrivacyEventsV1(ctx)
	require.NoError(t, err)
	state.PrivacyScanSummaries, state.PrivacyScanOutputs, err = k.ExportGenesisPrivacyScanV2(ctx)
	require.NoError(t, err)
	state.PrivacyGlobalSequence, err = k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	metadata.State, metadata.AssetRegistry = state, state.AssetRegistry
	require.Equal(t, uint64(11), metadata.RestartHeight)
	require.NoError(t, metadata.Validate())

	restored, restoredCtx, _ := setupMsgServerKeeper()
	restoredCtx = restoredCtx.WithChainID(ctx.ChainID()).WithBlockHeight(int64(metadata.RestartHeight))
	restored.audit = &auditRuntime{identity: identity, initialHeight: 1}
	require.NoError(t, auditinit.Run(restoredCtx, func(init sdk.Context) error {
		if err := restored.SetCircuitSetIdentity(init, identity); err != nil {
			return err
		}
		if err := restored.InitializeAuditMetadata(init, metadata, anchor); err != nil {
			return err
		}
		if err := restored.InitGenesisAssetRegistryV1(init, state.AssetRegistry); err != nil {
			return err
		}
		if err := restored.InitGenesisNullifiers(init, state.Nullifiers); err != nil {
			return err
		}
		if err := restored.InitGenesisReserveBalancesV1(init, state.ReserveBalances); err != nil {
			return err
		}
		return restored.InitGenesisPrivacyIndexV2(init, state.PrivacyGlobalSequence, state.PrivacyEvents, state.PrivacyScanSummaries, state.PrivacyScanOutputs)
	}))

	for epoch := uint64(1); epoch <= 4; epoch++ {
		_, _, err := restored.getAuditKey(restoredCtx, epoch)
		require.NoError(t, err)
	}
	activeKey, err := restored.activeAuditKey(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), activeKey.Epoch)
	pendingEpoch, pendingHeight, err := restored.pendingAuditEpoch(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), pendingEpoch)
	require.Equal(t, uint64(20), pendingHeight)
	halted, err := restored.auditHalted(restoredCtx)
	require.NoError(t, err)
	require.True(t, halted)
	reserve, err := restored.GetReserveSnapshot(restoredCtx, "uclair")
	require.NoError(t, err)
	require.Equal(t, "9", reserve.TotalDeposited.String())
	summaries, _, err := restored.ExportGenesisPrivacyScanV2(restoredCtx)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
}

func auditGenesisTestIdentity() *types.CircuitSetIdentity {
	identity := &types.CircuitSetIdentity{SchemaVersion: types.CircuitSetIdentitySchemaVersion, CircuitSetId: types.AuditFieldCircuitSetID, Curve: types.CircuitCurveBN254}
	for _, id := range types.RequiredAuditCircuitIdentityOrder {
		identity.Circuits = append(identity.Circuits, &types.CircuitIdentity{CircuitId: id, VerifyingKeySha256: strings.Repeat("a", 64), PublicInputSchemaSha256: strings.Repeat("b", 64)})
	}
	return identity
}

func auditGenesisTestKey(t *testing.T, k *Keeper, ctx sdk.Context, scalar int64, epoch, activation, registered uint64, origin auditOrigin) *auditKeyRecord {
	t.Helper()
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(big.NewInt(scalar).FillBytes(make([]byte, 32)))
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	require.NoError(t, err)
	network, err := k.auditNetwork(ctx)
	require.NoError(t, err)
	pop, err := privacycrypto.CreateAuditPoP96(secret, network, epoch, activation)
	require.NoError(t, err)
	popBytes, err := pop.Bytes()
	require.NoError(t, err)
	point := key.Point()
	pointBytes := point.Bytes()
	return &auditKeyRecord{Epoch: epoch, KeyID: key.ID(), Suite: 2, PK: pointBytes, PoP: popBytes, ActivationHeight: activation, RegisteredHeight: registered, Origin: origin}
}
