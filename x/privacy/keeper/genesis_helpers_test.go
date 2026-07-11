package keeper

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestInitGenesisCommitmentsRejectsCapacityOverflow(t *testing.T) {
	k, ctx := setupTreeKeeper()
	k.SetLeafCount(ctx, MaxMerkleLeaves)

	err := k.InitGenesisCommitments(ctx, [][]byte{fixedFieldBytes(80)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "genesis commitments exceed merkle tree capacity")
	require.Equal(t, MaxMerkleLeaves, k.GetLeafCount(ctx))
}

func TestInitGenesisCommitmentsRejectsDuplicateWithoutChangingPrefix(t *testing.T) {
	k, ctx := setupTreeKeeper()
	commitment := fixedFieldBytes(81)

	err := k.InitGenesisCommitments(ctx, [][]byte{commitment, commitment})
	require.ErrorContains(t, err, "duplicates index 0")
	require.Equal(t, uint64(0), k.GetLeafCount(ctx))
	require.Empty(t, k.GetLeaf(ctx, 0))
}

func TestInitGenesisHistoricalRootsRejectsForgedRoot(t *testing.T) {
	k, ctx := setupTreeKeeper()
	require.NoError(t, k.InitGenesisCommitments(ctx, [][]byte{fixedFieldBytes(82)}))

	err := k.InitGenesisHistoricalRoots(ctx, [][]byte{fixedFieldBytes(999)})
	require.ErrorContains(t, err, "does not match any commitment prefix root")
	require.False(t, k.CheckHistoricalRoot(ctx, fixedFieldBytes(999)))
}

func TestInitGenesisHistoricalRootsAcceptsRecomputedPrefixRoot(t *testing.T) {
	k, ctx := setupTreeKeeper()
	require.NoError(t, k.InitGenesisCommitments(ctx, [][]byte{fixedFieldBytes(83)}))
	root := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)

	require.NoError(t, k.InitGenesisHistoricalRoots(ctx, [][]byte{root}))
}

func TestSession2FoundationGenesisHelpersRoundTripLosslessly(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(70)
	commitment := fixedFieldBytes(290)
	require.NoError(t, k.AppendCommitment(ctx, commitment))
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeDeposit, indexedTxHashHex(0xee), []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyCommitment, fmt.Sprintf("%x", commitment)),
		sdk.NewAttribute(privacytypes.AttributeKeyEncryptedNote, "c0ffee"),
	}))
	_, err := k.RegisterCanonicalAssetV1(ctx, "uclair")
	require.NoError(t, err)
	require.NoError(t, k.RecordReserveDeposit(ctx, sdk.NewInt64Coin("uclair", 30)))
	require.NoError(t, k.RecordReserveWithdraw(ctx, sdk.NewInt64Coin("uclair", 7)))

	commitments, err := k.ExportGenesisCommitments(ctx)
	require.NoError(t, err)
	historicalRoots, err := k.ExportGenesisHistoricalRoots(ctx)
	require.NoError(t, err)
	rootSnapshots, err := k.ExportGenesisMerkleRootSnapshotsV1(ctx)
	require.NoError(t, err)
	assets, err := k.ExportGenesisAssetRegistryV1(ctx)
	require.NoError(t, err)
	reserves, err := k.ExportGenesisReserveBalancesV1(ctx)
	require.NoError(t, err)
	events, err := k.ExportGenesisPrivacyEventsV1(ctx)
	require.NoError(t, err)
	summaries, outputs, err := k.ExportGenesisPrivacyScanV2(ctx)
	require.NoError(t, err)
	sequence, err := k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)

	restored, restoredCtx, _ := setupMsgServerKeeper()
	require.NoError(t, restored.InitGenesisCommitments(restoredCtx, commitments))
	require.NoError(t, restored.InitGenesisHistoricalRoots(restoredCtx, historicalRoots))
	require.NoError(t, restored.InitGenesisMerkleRootSnapshotsV1(restoredCtx, rootSnapshots))
	require.NoError(t, restored.InitGenesisAssetRegistryV1(restoredCtx, assets))
	require.NoError(t, restored.InitGenesisReserveBalancesV1(restoredCtx, reserves))
	require.NoError(t, restored.InitGenesisPrivacyIndexV2(restoredCtx, sequence, events, summaries, outputs))

	restoredSnapshots, err := restored.ExportGenesisMerkleRootSnapshotsV1(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, rootSnapshots, restoredSnapshots)
	restoredAssets, err := restored.ExportGenesisAssetRegistryV1(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, assets, restoredAssets)
	restoredReserves, err := restored.ExportGenesisReserveBalancesV1(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, reserves, restoredReserves)
	restoredEvents, err := restored.ExportGenesisPrivacyEventsV1(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, events, restoredEvents)
	restoredSummaries, restoredOutputs, err := restored.ExportGenesisPrivacyScanV2(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, summaries, restoredSummaries)
	require.Equal(t, outputs, restoredOutputs)
	restoredSequence, err := restored.GetPrivacyGlobalSequence(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, sequence, restoredSequence)
}
