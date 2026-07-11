package keeper

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBatchScanIndexStoresPayloadOnceAndEmitsMinimalSummary(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	ctx = ctx.WithBlockHeight(77).WithTxBytes([]byte("batch-scan-index-test"))
	msg := testMaxBatchTransferMessage(t)
	derived, err := deriveBatchPublicV1(ctx, msg)
	require.NoError(t, err)

	for _, output := range msg.Outputs {
		require.NoError(t, k.AppendCommitment(ctx, output.Commitment))
	}
	require.NoError(t, k.storeBatchPrivacyEffectV1(ctx, msg, derived.effect))

	page, err := k.GetPrivacyScanPageV2(ctx, nil, privacytypes.BatchJoinSplitV1MaxOutputs, 1, MaxPrivacyScanByteLimit, nil)
	require.NoError(t, err)
	require.Len(t, page.Summaries, 1)
	require.Len(t, page.Outputs, int(privacytypes.BatchJoinSplitV1MaxOutputs))
	require.False(t, page.HasMore)
	summary := page.Summaries[0]
	require.Equal(t, privacytypes.EventTypeBatchTransferV1, summary.EventType)
	require.Equal(t, derived.effect.effectID, summary.EffectId)
	require.Equal(t, msg.Nullifiers, summary.Nullifiers)
	require.Equal(t, msg.AuditKeyId, summary.AuditKeyId)
	require.Equal(t, msg.AuditKeyEpoch, summary.AuditKeyEpoch)

	for i, output := range page.Outputs {
		require.Equal(t, uint32(i), output.OutputIndex)
		require.Equal(t, msg.Outputs[i].Commitment, output.Commitment)
		require.Equal(t, msg.Outputs[i].Ciphertext, output.Ciphertext)
		require.Equal(t, msg.Outputs[i].UserDisclosurePayload, output.UserDisclosurePayload)
		require.Equal(t, msg.Outputs[i].AuditDisclosurePayload, output.AuditDisclosurePayload)
		require.Equal(t, msg.Outputs[i].SelfViewDisclosurePayload, output.SelfViewDisclosurePayload)
	}

	events, hasMore, err := k.GetPrivacyEvents(ctx, 0, 1, 10, []string{privacytypes.EventTypeBatchTransferV1})
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Len(t, events, 1)
	require.Equal(t, summary.GlobalSequence, events[0].Sequence)
	require.Len(t, events[0].Attributes, len(batchMinimalEventAttributes(msg, derived.effect)))
	forbiddenKeys := map[string]struct{}{
		privacytypes.AttributeKeyCipherText1: {}, privacytypes.AttributeKeyCipherText2: {},
		privacytypes.AttributeKeyUserDisclosurePayload: {}, privacytypes.AttributeKeyAuditDisclosurePayload: {},
		privacytypes.AttributeKeySelfViewDisclosurePayload: {}, privacytypes.AttributeKeyCommitment1: {},
		privacytypes.AttributeKeyCommitment2: {}, privacytypes.AttributeKeyNullifier1: {}, privacytypes.AttributeKeyNullifier2: {},
	}
	for _, attr := range events[0].Attributes {
		_, forbidden := forbiddenKeys[attr.Key]
		require.False(t, forbidden, attr.Key)
		require.NotEqual(t, hex.EncodeToString(msg.Outputs[0].Ciphertext), attr.Value)
		require.NotEqual(t, hex.EncodeToString(msg.Outputs[0].AuditDisclosurePayload), attr.Value)
	}

	abciEvents := ctx.EventManager().Events()
	require.Len(t, abciEvents, 1)
	require.Equal(t, privacytypes.EventTypeBatchTransferV1, abciEvents[0].Type)
	require.Len(t, abciEvents[0].Attributes, len(events[0].Attributes))

	sequence, err := k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sequence)
}

func BenchmarkPrivacyScanMaxBatchPage(b *testing.B) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(b)
	ctx = ctx.WithBlockHeight(77).WithTxBytes([]byte("batch-scan-throughput-benchmark"))
	msg := testMaxBatchTransferMessage(b)
	derived, err := deriveBatchPublicV1(ctx, msg)
	require.NoError(b, err)
	for _, output := range msg.Outputs {
		require.NoError(b, k.AppendCommitment(ctx, output.Commitment))
	}
	require.NoError(b, k.storeBatchPrivacyEffectV1(ctx, msg, derived.effect))
	page, err := k.GetPrivacyScanPageV2(ctx, nil, privacytypes.BatchJoinSplitV1MaxOutputs, 1, MaxPrivacyScanByteLimit, nil)
	require.NoError(b, err)
	encoded, err := page.Marshal()
	require.NoError(b, err)
	b.ReportAllocs()
	b.ReportMetric(float64(len(encoded)), "response-bytes/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err = k.GetPrivacyScanPageV2(ctx, nil, privacytypes.BatchJoinSplitV1MaxOutputs, 1, MaxPrivacyScanByteLimit, nil)
		if err != nil || len(page.Outputs) != int(privacytypes.BatchJoinSplitV1MaxOutputs) {
			b.Fatalf("scan max batch page: outputs=%d err=%v", len(page.Outputs), err)
		}
	}
	b.ReportMetric(float64(b.N*int(privacytypes.BatchJoinSplitV1MaxOutputs))/b.Elapsed().Seconds(), "outputs/s")
}

func TestBatchPublicWitnessIsDerivedInFrozenOrder(t *testing.T) {
	_, ctx, _ := setupRegisteredMsgServerKeeper(t)
	msg := testMaxBatchTransferMessage(t)
	derived, err := deriveBatchPublicV1(ctx, msg)
	require.NoError(t, err)

	publicWitness, err := newPublicWitnessBN254(&derived.assignment)
	require.NoError(t, err)
	values := publicWitness.Vector().(fr.Vector)
	require.Len(t, values, 12)

	// Relayer and proof changes are deliberately absent from all derived public
	// values; output order is effect data and must change the public statement.
	msg.Creator = testAddress(0x42)
	msg.Proof = testBatchProofFrame()
	relayerChanged, err := deriveBatchPublicV1(ctx, msg)
	require.NoError(t, err)
	require.Equal(t, derived.effect, relayerChanged.effect)

	msg.Outputs[0], msg.Outputs[1] = msg.Outputs[1], msg.Outputs[0]
	reordered, err := deriveBatchPublicV1(ctx, msg)
	require.NoError(t, err)
	require.NotEqual(t, derived.effect.commitmentRoot, reordered.effect.commitmentRoot)
	require.NotEqual(t, derived.effect.effectID, reordered.effect.effectID)
}

func TestBatchPublicWitnessShapePropertyCoversEveryOutputCount(t *testing.T) {
	_, ctx, _ := setupRegisteredMsgServerKeeper(t)
	base := testMaxBatchTransferMessage(t)
	for outputCount := 1; outputCount <= int(privacytypes.BatchJoinSplitV1MaxOutputs); outputCount++ {
		inputCount := (outputCount-1)%int(privacytypes.BatchJoinSplitV1MaxInputs) + 1
		t.Run(big.NewInt(int64(outputCount)).String(), func(t *testing.T) {
			msg := *base
			msg.Nullifiers = base.Nullifiers[:inputCount]
			msg.Outputs = base.Outputs[:outputCount]
			derived, err := deriveBatchPublicV1(ctx, &msg)
			require.NoError(t, err)

			publicWitness, err := newPublicWitnessBN254(&derived.assignment)
			require.NoError(t, err)
			values := publicWitness.Vector().(fr.Vector)
			expected := []*big.Int{
				derived.assignment.MerkleRoot.(*big.Int),
				derived.assignment.ChainDomainHi.(*big.Int),
				derived.assignment.ChainDomainLo.(*big.Int),
				derived.assignment.ExpiresAtUnix.(*big.Int),
				derived.assignment.InputCount.(*big.Int),
				derived.assignment.OutputCount.(*big.Int),
				derived.assignment.NullifierRoot.(*big.Int),
				derived.assignment.CommitmentRoot.(*big.Int),
				derived.assignment.UserDisclosureRoot.(*big.Int),
				derived.assignment.FullDisclosureRoot.(*big.Int),
				derived.assignment.PayloadDigestHi.(*big.Int),
				derived.assignment.PayloadDigestLo.(*big.Int),
			}
			require.Len(t, values, len(expected))
			for i := range expected {
				var want fr.Element
				want.SetBigInt(expected[i])
				require.Truef(t, values[i].Equal(&want), "public witness index %d (%s)", i, privacytypes.BatchPublicInputOrderV1[i])
			}
			require.Equal(t, uint64(inputCount), derived.assignment.InputCount.(*big.Int).Uint64())
			require.Equal(t, uint64(outputCount), derived.assignment.OutputCount.(*big.Int).Uint64())
			require.Equal(t, batchFieldBytes32(expected[6]), derived.effect.nullifierRoot)
			require.Equal(t, batchFieldBytes32(expected[7]), derived.effect.commitmentRoot)
			require.Equal(t, batchFieldBytes32(expected[8]), derived.effect.userDisclosureRoot)
			require.Equal(t, batchFieldBytes32(expected[9]), derived.effect.fullDisclosureRoot)
		})
	}
}

func TestDepositJoinSplitAndBatchShareGlobalPrivacySequence(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	ctx = ctx.WithBlockHeight(70)
	indexTestDepositV2(t, k, ctx, fixedFieldBytes(231), 0xa1)
	ctx = ctx.WithBlockHeight(71)
	indexTestTransferV2(t, k, ctx, fixedFieldBytes(232), fixedFieldBytes(233), 0xa2)

	ctx = ctx.WithBlockHeight(72).WithTxBytes([]byte("shared-global-sequence-batch"))
	msg := testMaxBatchTransferMessage(t)
	derived, err := deriveBatchPublicV1(ctx, msg)
	require.NoError(t, err)
	for _, output := range msg.Outputs {
		require.NoError(t, k.AppendCommitment(ctx, output.Commitment))
	}
	require.NoError(t, k.storeBatchPrivacyEffectV1(ctx, msg, derived.effect))

	sequence, err := k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), sequence)
	page, err := k.GetPrivacyScanPageV2(ctx, nil, 35, 3, MaxPrivacyScanByteLimit, nil)
	require.NoError(t, err)
	require.Len(t, page.Summaries, 3)
	require.Len(t, page.Outputs, 35)
	require.Equal(t, []uint64{1, 2, 3}, []uint64{
		page.Summaries[0].GlobalSequence,
		page.Summaries[1].GlobalSequence,
		page.Summaries[2].GlobalSequence,
	})
	require.Equal(t, []string{
		privacytypes.EventTypeDeposit,
		privacytypes.EventTypeShieldedTransfer,
		privacytypes.EventTypeBatchTransferV1,
	}, []string{
		page.Summaries[0].EventType,
		page.Summaries[1].EventType,
		page.Summaries[2].EventType,
	})
}

func TestBatchScanGenesisRoundTripPreservesCursorLeafAndSequence(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	ctx = ctx.WithBlockHeight(88).WithTxBytes([]byte("batch-scan-genesis-roundtrip"))
	msg := testMaxBatchTransferMessage(t)
	msg.Nullifiers = msg.Nullifiers[:1]
	msg.Outputs = msg.Outputs[:2]
	derived, err := deriveBatchPublicV1(ctx, msg)
	require.NoError(t, err)
	for _, output := range msg.Outputs {
		require.NoError(t, k.AppendCommitment(ctx, output.Commitment))
	}
	finalRoot := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	require.NoError(t, k.storeBatchPrivacyEffectV1(ctx, msg, derived.effect))

	commitments, err := k.ExportGenesisCommitments(ctx)
	require.NoError(t, err)
	historicalRoots, err := k.ExportGenesisHistoricalRoots(ctx)
	require.NoError(t, err)
	rootSnapshots, err := k.ExportGenesisMerkleRootSnapshotsV1(ctx)
	require.NoError(t, err)
	events, err := k.ExportGenesisPrivacyEventsV1(ctx)
	require.NoError(t, err)
	summaries, outputs, err := k.ExportGenesisPrivacyScanV2(ctx)
	require.NoError(t, err)
	sequence, err := k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sequence)

	restored, restoredCtx, _ := setupRegisteredMsgServerKeeper(t)
	require.NoError(t, restored.InitGenesisCommitments(restoredCtx, commitments))
	require.NoError(t, restored.InitGenesisHistoricalRoots(restoredCtx, historicalRoots))
	require.NoError(t, restored.InitGenesisMerkleRootSnapshotsV1(restoredCtx, rootSnapshots))
	require.NoError(t, restored.InitGenesisPrivacyIndexV2(restoredCtx, sequence, events, summaries, outputs))

	restoredSummaries, restoredOutputs, err := restored.ExportGenesisPrivacyScanV2(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, summaries, restoredSummaries)
	require.Equal(t, outputs, restoredOutputs)
	for _, root := range historicalRoots {
		require.True(t, restored.CheckHistoricalRoot(restoredCtx, root))
	}
	pathResponse, err := restored.CommitmentPathsAtRoot(sdk.WrapSDKContext(restoredCtx), &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: []string{
			hex.EncodeToString(msg.Outputs[0].Commitment),
			hex.EncodeToString(msg.Outputs[1].Commitment),
		},
		RootHex:        hex.EncodeToString(finalRoot),
		SnapshotHeight: 88,
	})
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(finalRoot), pathResponse.RootHex)
	require.Equal(t, int64(88), pathResponse.SnapshotHeight)
	require.Equal(t, uint64(2), pathResponse.LeafCount)
	require.Len(t, pathResponse.Paths, 2)
	for _, path := range pathResponse.Paths {
		commitment, decodeErr := hex.DecodeString(path.CommitmentHex)
		require.NoError(t, decodeErr)
		reconstructed, rebuildErr := rootFromPath(commitment, path.Path, path.PathHelper)
		require.NoError(t, rebuildErr)
		require.Equal(t, finalRoot, reconstructed)
	}
	page, err := restored.GetPrivacyScanPageV2(restoredCtx, nil, 2, 1, MaxPrivacyScanByteLimit, nil)
	require.NoError(t, err)
	require.Len(t, page.Summaries, 1)
	require.Len(t, page.Outputs, 2)
	require.Equal(t, uint64(0), page.Outputs[0].LeafIndex)
	require.Equal(t, uint64(1), page.Outputs[1].LeafIndex)
	require.Equal(t, &privacytypes.PrivacyScanCursorV1{
		Height: 88, GlobalSequence: 1, OutputIndex: 1,
	}, page.NextCursor)

	next, err := restored.AllocatePrivacyGlobalSequence(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), next)
}
