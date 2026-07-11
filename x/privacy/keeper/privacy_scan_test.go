package keeper

import (
	"bytes"
	"fmt"
	"math/big"
	"strings"
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
		sdk.NewAttribute(privacytypes.AttributeKeyEncryptedNote, fmt.Sprintf("%x", testKeeperEnvelope(t, privacytypes.EnvelopeDepositNoteV1))),
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
		sdk.NewAttribute(privacytypes.AttributeKeyCipherText1, fmt.Sprintf("%x", testKeeperEnvelope(t, privacytypes.EnvelopeTransferNoteV1))),
		sdk.NewAttribute(privacytypes.AttributeKeyCipherText2, fmt.Sprintf("%x", testKeeperEnvelope(t, privacytypes.EnvelopeTransferNoteV1))),
		sdk.NewAttribute(privacytypes.AttributeKeyViewTag1, "0102"),
		sdk.NewAttribute(privacytypes.AttributeKeyViewTag2, "0304"),
		sdk.NewAttribute(privacytypes.AttributeKeyUserPrivacyPolicy, "0"),
		sdk.NewAttribute(privacytypes.AttributeKeyUserDisclosureMode, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE.String()),
		sdk.NewAttribute(privacytypes.AttributeKeyAuditDisclosureDigest, fmt.Sprintf("%x", fixedFieldBytes(243))),
		sdk.NewAttribute(privacytypes.AttributeKeyAuditDisclosureTargetPubKey, fmt.Sprintf("%x", testKeeperDisclosurePubKey())),
		sdk.NewAttribute(privacytypes.AttributeKeyAuditDisclosurePayload, fmt.Sprintf("%x", testKeeperEnvelope(t, privacytypes.EnvelopeAuditDisclosureV1))),
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

func TestPrivacyScanV2DoesNotAdvanceOutputPagePastFilteredTail(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(46)
	indexTestDepositV2(t, k, ctx, fixedFieldBytes(246), 0xbd)
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeWithdraw, indexedTxHashHex(0xbe), []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier, fmt.Sprintf("%x", fixedFieldBytes(247))),
	}))

	filter := []string{privacytypes.EventTypeDeposit}
	first, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{EventTypes: filter})
	require.NoError(t, err)
	require.Len(t, first.Outputs, 1)
	require.True(t, first.HasMore)
	require.Equal(t, &privacytypes.PrivacyScanCursorV1{Height: 46, GlobalSequence: 1, OutputIndex: 0}, first.NextCursor)

	second, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{After: first.NextCursor, EventTypes: filter})
	require.NoError(t, err)
	require.Empty(t, second.Summaries)
	require.Empty(t, second.Outputs)
	require.False(t, second.HasMore)
	require.Equal(t, &privacytypes.PrivacyScanCursorV1{Height: 46, GlobalSequence: 2, OutputIndex: 0}, second.NextCursor)
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

func TestPrivacyScanV2RejectsCorruptExactOutputContracts(t *testing.T) {
	t.Run("wrong fixed envelope", func(t *testing.T) {
		k, ctx, _ := setupMsgServerKeeper()
		ctx = ctx.WithBlockHeight(61)
		indexTestDepositV2(t, k, ctx, fixedFieldBytes(262), 0xde)
		store := k.storeService.OpenKVStore(ctx)
		key := privacytypes.GetPrivacyScanOutputKey(61, 1, 0)
		encoded, err := store.Get(key)
		require.NoError(t, err)
		var output privacytypes.PrivacyScanOutputV2
		require.NoError(t, output.Unmarshal(encoded))
		output.EncryptedNote[0] ^= 0xff
		encoded, err = output.Marshal()
		require.NoError(t, err)
		require.NoError(t, store.Set(key, encoded))

		response, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{})
		require.Nil(t, response)
		require.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("stray non-adjacent output", func(t *testing.T) {
		k, ctx, _ := setupMsgServerKeeper()
		ctx = ctx.WithBlockHeight(62)
		indexTestDepositV2(t, k, ctx, fixedFieldBytes(263), 0xdf)
		store := k.storeService.OpenKVStore(ctx)
		encoded, err := store.Get(privacytypes.GetPrivacyScanOutputKey(62, 1, 0))
		require.NoError(t, err)
		var output privacytypes.PrivacyScanOutputV2
		require.NoError(t, output.Unmarshal(encoded))
		output.OutputIndex = 2
		encoded, err = output.Marshal()
		require.NoError(t, err)
		require.NoError(t, store.Set(privacytypes.GetPrivacyScanOutputKey(62, 1, 2), encoded))

		response, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{})
		require.Nil(t, response)
		require.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("invalid audit point and digest", func(t *testing.T) {
		k, ctx, _ := setupMsgServerKeeper()
		ctx = ctx.WithBlockHeight(63)
		indexTestTransferV2(t, k, ctx, fixedFieldBytes(264), fixedFieldBytes(265), 0xe0)
		store := k.storeService.OpenKVStore(ctx)
		summaryKey := privacytypes.GetPrivacyScanSummaryKey(63, 1)
		encoded, err := store.Get(summaryKey)
		require.NoError(t, err)
		var summary privacytypes.PrivacyScanSummaryV2
		require.NoError(t, summary.Unmarshal(encoded))
		summary.AuditTargetPubkey = make([]byte, 32)
		encoded, err = summary.Marshal()
		require.NoError(t, err)
		require.NoError(t, store.Set(summaryKey, encoded))

		response, err := k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{})
		require.Nil(t, response)
		require.Equal(t, codes.Internal, status.Code(err))

		summary.AuditTargetPubkey = testKeeperDisclosurePubKey()
		encoded, err = summary.Marshal()
		require.NoError(t, err)
		require.NoError(t, store.Set(summaryKey, encoded))
		outputKey := privacytypes.GetPrivacyScanOutputKey(63, 1, 0)
		encoded, err = store.Get(outputKey)
		require.NoError(t, err)
		var output privacytypes.PrivacyScanOutputV2
		require.NoError(t, output.Unmarshal(encoded))
		output.FullDisclosureDigest = make([]byte, 32)
		encoded, err = output.Marshal()
		require.NoError(t, err)
		require.NoError(t, store.Set(outputKey, encoded))

		response, err = k.PrivacyScan(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyScanRequest{})
		require.Nil(t, response)
		require.Equal(t, codes.Internal, status.Code(err))
	})
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

func TestPrivacyScanV2AcceptsExactBatchPublicDisclosureContract(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(80)
	commitment := fixedFieldBytes(401)
	require.NoError(t, k.AppendCommitment(ctx, commitment))

	senderSpendX, senderSpendY := testKeeperPointBigInts(testKeeperScalarMulBase(big.NewInt(17)))
	senderViewX, senderViewY := testKeeperPointBigInts(testKeeperScalarMulBase(big.NewInt(19)))
	recipientSpendX, recipientSpendY := testKeeperPointBigInts(testKeeperScalarMulBase(big.NewInt(23)))
	recipientViewX, recipientViewY := testKeeperPointBigInts(testKeeperScalarMulBase(big.NewInt(29)))
	policy := privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom
	plaintext := &privacytypes.DisclosurePlaintextV1{
		Plane: privacytypes.DisclosurePlaneUserV1, OutputIndex: 0,
		Policy: policy, DisclosedFieldBitmap: policy,
		Commitment: new(big.Int).SetBytes(commitment), Amount: big.NewInt(7), AssetID: privacytypes.ComputeAssetIDV1("uclair"),
		SenderSpendKeyX: senderSpendX, SenderSpendKeyY: senderSpendY,
		SenderViewKeyX: senderViewX, SenderViewKeyY: senderViewY,
		RecipientSpendKeyX: recipientSpendX, RecipientSpendKeyY: recipientSpendY,
		RecipientViewKeyX: recipientViewX, RecipientViewKeyY: recipientViewY,
		DisclosureBlinding: big.NewInt(43),
	}
	publicPayload, err := privacytypes.MarshalDisclosurePlaintextV1(plaintext)
	require.NoError(t, err)
	userDigest, err := privacytypes.ComputeBatchUserDisclosureDigestV1(privacytypes.BatchUserDisclosureV1Input{
		OutputIndex: 0, Commitment: plaintext.Commitment, Policy: policy, DisclosedFieldBitmap: policy,
		SelectedAmount: plaintext.Amount, AssetID: plaintext.AssetID,
		SelectedFromSpendKeyX: senderSpendX, SelectedFromSpendKeyY: senderSpendY,
		SelectedFromViewKeyX: senderViewX, SelectedFromViewKeyY: senderViewY,
		SelectedToSpendKeyX: recipientSpendX, SelectedToSpendKeyY: recipientSpendY,
		SelectedToViewKeyX: recipientViewX, SelectedToViewKeyY: recipientViewY,
		UserDisclosureBlinding: plaintext.DisclosureBlinding,
	})
	require.NoError(t, err)

	auditID := strings.Repeat("a", privacytypes.AuditKeyIDV1MaxBytes)
	auditTarget := testKeeperDisclosurePubKey()
	effectID := bytes.Repeat([]byte{0x91}, 32)
	summary := &privacytypes.PrivacyScanSummaryV2{
		GlobalSequence: 1, Height: 80, EventType: privacytypes.EventTypeBatchTransferV1,
		Nullifiers: [][]byte{fixedFieldBytes(402)}, OutputCount: 1,
		CircuitSetId: privacytypes.ActiveCircuitSetID, PayloadVersion: privacytypes.FixedPayloadVersionV1,
		ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
		AuditKeyId:        auditID, AuditKeyEpoch: 1, AuditTargetPubkey: auditTarget, EffectId: effectID,
	}
	output := &privacytypes.PrivacyScanOutputV2{
		GlobalSequence: 1, Height: 80, OutputIndex: 0, EffectId: effectID,
		Commitment: commitment, Ciphertext: testKeeperEnvelope(t, privacytypes.EnvelopeTransferNoteV1),
		ViewTag: []byte{1, 2}, LeafIndex: 0, LeafIndexFound: true,
		UserPrivacyPolicy: policy, UserDisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC.String(),
		UserDisclosureDigest: userDigest.FillBytes(make([]byte, 32)), UserDisclosurePayload: publicPayload,
		FullDisclosureDigest: fixedFieldBytes(403), AuditDisclosurePayload: testKeeperEnvelope(t, privacytypes.EnvelopeAuditDisclosureV1),
		CircuitSetId: summary.CircuitSetId, PayloadVersion: summary.PayloadVersion, ScanSchemaVersion: summary.ScanSchemaVersion,
		AuditKeyId: auditID, AuditKeyEpoch: 1, AuditTargetPubkey: auditTarget,
		EventType: summary.EventType,
	}
	require.NoError(t, k.StorePrivacyScanV2(ctx, summary, []*privacytypes.PrivacyScanOutputV2{output}))

	page, err := k.GetPrivacyScanPageV2(ctx, nil, 0, 0, 0, nil)
	require.NoError(t, err)
	require.Len(t, page.Summaries, 1)
	require.Len(t, page.Outputs, 1)
	require.Equal(t, auditID, page.Summaries[0].AuditKeyId)
}
