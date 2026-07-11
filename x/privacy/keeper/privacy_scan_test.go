package keeper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func indexTestDepositV2(t *testing.T, k *Keeper, ctx sdk.Context, commitment []byte, marker byte) {
	t.Helper()
	require.NoError(t, k.AppendCommitment(ctx, commitment))
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeDeposit, indexedTxHashHex(marker), []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyCommitment, fmt.Sprintf("%x", commitment)),
		sdk.NewAttribute(privacytypes.AttributeKeyEncryptedNote, "deadbeef"),
	}))
}

func indexTestTransferV2(t *testing.T, k *Keeper, ctx sdk.Context, first, second []byte, marker byte) {
	t.Helper()
	require.NoError(t, k.AppendCommitment(ctx, first))
	require.NoError(t, k.AppendCommitment(ctx, second))
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeShieldedTransfer, indexedTxHashHex(marker), []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier1, fmt.Sprintf("%x", fixedFieldBytes(241))),
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier2, fmt.Sprintf("%x", fixedFieldBytes(242))),
		sdk.NewAttribute(privacytypes.AttributeKeyCommitment1, fmt.Sprintf("%x", first)),
		sdk.NewAttribute(privacytypes.AttributeKeyCommitment2, fmt.Sprintf("%x", second)),
		sdk.NewAttribute(privacytypes.AttributeKeyCipherText1, "c0ffee"),
		sdk.NewAttribute(privacytypes.AttributeKeyCipherText2, "decafbad"),
		sdk.NewAttribute(privacytypes.AttributeKeyViewTag1, "0102"),
		sdk.NewAttribute(privacytypes.AttributeKeyViewTag2, "0304"),
	}))
}

func TestPrivacyScanV2CursorResumesInsideMultiOutputEvent(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(40)
	indexTestDepositV2(t, k, ctx, fixedFieldBytes(231), 0xaa)
	ctx = ctx.WithBlockHeight(41)
	indexTestTransferV2(t, k, ctx, fixedFieldBytes(232), fixedFieldBytes(233), 0xbb)

	first, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{OutputLimit: 1, EventLimit: 1})
	require.NoError(t, err)
	require.Len(t, first.Outputs, 1)
	require.True(t, first.HasMore)
	require.Equal(t, &privacytypes.PrivacyScanCursorV1{Height: 40, GlobalSequence: 1, OutputIndex: 0}, first.NextCursor)
	require.Equal(t, privacytypes.PrivacyScanSchemaVersionV2, first.ScanSchemaVersion)

	second, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{
		After:       first.NextCursor,
		OutputLimit: 1,
		EventLimit:  1,
	})
	require.NoError(t, err)
	require.Len(t, second.Outputs, 1)
	require.Len(t, second.Summaries, 1)
	require.Equal(t, uint32(2), second.Summaries[0].OutputCount)
	require.Equal(t, uint32(0), second.Outputs[0].OutputIndex)
	// A page can transport a prefix, but output_count + has_more makes it
	// impossible for a consumer to report the batch as a complete item yet.
	require.Less(t, len(second.Outputs), int(second.Summaries[0].OutputCount))
	require.True(t, second.HasMore)
	require.Equal(t, &privacytypes.PrivacyScanCursorV1{Height: 41, GlobalSequence: 2, OutputIndex: 0}, second.NextCursor)

	third, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{
		After:       second.NextCursor,
		OutputLimit: 1,
		EventLimit:  1,
	})
	require.NoError(t, err)
	require.Len(t, third.Outputs, 1)
	require.Equal(t, uint32(1), third.Outputs[0].OutputIndex)
	require.False(t, third.HasMore)
	require.Equal(t, &privacytypes.PrivacyScanCursorV1{Height: 41, GlobalSequence: 2, OutputIndex: 1}, third.NextCursor)
}

func TestPrivacyScanV2ReturnsZeroOutputWithdrawSummary(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(45)
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeWithdraw, indexedTxHashHex(0xbc), []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier, fmt.Sprintf("%x", fixedFieldBytes(245))),
	}))

	page, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{})
	require.NoError(t, err)
	require.Len(t, page.Summaries, 1)
	require.Empty(t, page.Outputs)
	require.Equal(t, privacytypes.EventTypeWithdraw, page.Summaries[0].EventType)
	require.Zero(t, page.Summaries[0].OutputCount)
	require.Equal(t, &privacytypes.PrivacyScanCursorV1{Height: 45, GlobalSequence: 1, OutputIndex: 0}, page.NextCursor)

	next, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{After: page.NextCursor})
	require.NoError(t, err)
	require.Empty(t, next.Summaries)
	require.Empty(t, next.Outputs)
}

func TestPrivacyScanV2EnforcesRecordEventAndByteBounds(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(50)
	indexTestDepositV2(t, k, ctx, fixedFieldBytes(251), 0xcc)

	_, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{MaxEncodedBytes: 1})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	_, err = k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{OutputLimit: MaxPrivacyScanOutputLimit + 1})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{EventLimit: MaxPrivacyScanEventLimit + 1})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{MaxEncodedBytes: MaxPrivacyScanByteLimit + 1})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPrivacyScanV2CorruptTypedRecordFailsClosed(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(60)
	indexTestDepositV2(t, k, ctx, fixedFieldBytes(261), 0xdd)
	require.NoError(t, k.storeService.OpenKVStore(ctx).Set(privacytypes.GetPrivacyScanOutputKey(60, 1, 0), []byte{0xff, 0xff}))

	response, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{})
	require.Nil(t, response)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestPrivacyGlobalSequenceCorruptionAndOverflowFailClosed(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	store := k.storeService.OpenKVStore(ctx)
	require.NoError(t, store.Set(privacytypes.GetPrivacyGlobalSequenceKey(), []byte{1}))
	_, err := k.AllocatePrivacyGlobalSequence(ctx)
	require.ErrorContains(t, err, "corrupt")

	max := make([]byte, 8)
	for i := range max {
		max[i] = 0xff
	}
	require.NoError(t, store.Set(privacytypes.GetPrivacyGlobalSequenceKey(), max))
	_, err = k.AllocatePrivacyGlobalSequence(ctx)
	require.ErrorContains(t, err, "exhausted")
}

func TestPrivacyScanV2RejectsGlobalSequenceReuseAcrossHeights(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	newSummary := func(height int64) *privacytypes.PrivacyScanSummaryV2 {
		return &privacytypes.PrivacyScanSummaryV2{
			GlobalSequence:    7,
			Height:            height,
			EventType:         privacytypes.EventTypeWithdraw,
			Nullifiers:        [][]byte{fixedFieldBytes(uint64(300 + height))},
			CircuitSetId:      privacytypes.ActiveCircuitSetID,
			PayloadVersion:    privacytypes.FixedPayloadVersionV1,
			ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
		}
	}
	require.NoError(t, k.StorePrivacyScanV2(ctx, newSummary(1), nil))
	err := k.StorePrivacyScanV2(ctx, newSummary(2), nil)
	require.ErrorContains(t, err, "global sequence is already indexed")
}
