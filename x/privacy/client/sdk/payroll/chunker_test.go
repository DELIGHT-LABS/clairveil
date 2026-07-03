package payroll

import (
	"context"
	"math/big"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestChunkProofResultsSplitsByMaxMessages(t *testing.T) {
	results := []ProofResult{
		testProofResult("op-1", "a"),
		testProofResult("op-2", "b"),
		testProofResult("op-3", "c"),
	}

	chunks, err := ChunkProofResults(results, ChunkOptions{MaxMessagesPerTx: 2, ChunkIDPrefix: "chunk"})
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, "chunk-000001", chunks[0].ChunkID)
	require.Len(t, chunks[0].Messages, 2)
	require.Equal(t, "chunk-000002", chunks[1].ChunkID)
	require.Len(t, chunks[1].Messages, 1)
}

func TestChunkProofResultsRejectsDuplicateNullifiers(t *testing.T) {
	_, err := ChunkProofResults([]ProofResult{
		testProofResult("op-1", "same"),
		testProofResult("op-2", "same"),
	}, ChunkOptions{MaxMessagesPerTx: 2})
	require.ErrorContains(t, err, "duplicate nullifier")
}

func TestBatchBroadcastWorkerSubmitsChunkOnce(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items = []PayrollItemInput{
		{ItemID: "item-1", EmployeeID: "employee-1", RecipientAddress: testRecipientAddress("1"), Amount: big.NewInt(70)},
		{ItemID: "item-2", EmployeeID: "employee-2", RecipientAddress: testRecipientAddress("2"), Amount: big.NewInt(50)},
	}
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large-1", "uclair", 100, false, ""),
		testTreasuryNote("zero-1", "uclair", 0, false, ""),
		testTreasuryNote("large-2", "uclair", 80, false, ""),
		testTreasuryNote("zero-2", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	proofWorker := ProofWorker{
		Reservation:    reservationService,
		PayloadBuilder: fakePayloadBuilder{},
		ProofRunner:    fakeProofRunner{},
		Assembler:      fakeAssembler{},
		LeaseOwner:     "proof-worker-a",
		LeaseTTL:       time.Minute,
	}
	results := make([]ProofResult, 0, len(confirmed.Items))
	for _, item := range confirmed.Items {
		result, err := proofWorker.Process(ctx, item)
		require.NoError(t, err)
		result.Message = &privacytypes.MsgTransfer{Nullifiers: [][]byte{[]byte(item.OperationID + "-a"), []byte(item.OperationID + "-b")}}
		results = append(results, *result)
	}
	chunks, err := ChunkProofResults(results, ChunkOptions{MaxMessagesPerTx: 10})
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}
	result, err := worker.SubmitChunk(ctx, chunks[0])
	require.NoError(t, err)
	require.Equal(t, "TXHASH", result.TxHash)
	require.Equal(t, 1, broadcaster.calls)
	require.Len(t, broadcaster.messageCounts, 1)
	require.Equal(t, 2, broadcaster.messageCounts[0])
}

func TestBatchBroadcastWorkerRejectsExpiredLeaseBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	now = now.Add(2 * time.Minute)
	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster, LeaseTTL: time.Minute}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, privacyreservation.ErrLeaseUnavailable)
	require.Equal(t, 0, broadcaster.calls)
}

func TestBatchBroadcastWorkerDoesNotPartiallySubmitChunkOnOperationFailure(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	chunk.Results[1].Item.OperationID = "missing-operation"

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, privacyreservation.ErrOperationNotFound)
	require.Equal(t, 0, broadcaster.calls)

	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		}
	}
}

func TestBatchBroadcastWorkerRejectsProofResultWithoutInputNotes(t *testing.T) {
	ctx := context.Background()
	reservationService := privacyreservation.Service{Store: privacyreservation.NewMemoryStore(), Now: testNow}
	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}

	_, err := worker.SubmitChunk(ctx, MessageChunk{
		ChunkID:  "chunk-1",
		Messages: []sdk.Msg{&privacytypes.MsgTransfer{}},
		Results: []ProofResult{{
			Item:              PayrollPlanItem{OperationID: "op-a"},
			ReservationLeases: map[string]string{},
		}},
	})
	require.ErrorContains(t, err, "has no input notes")
	require.Equal(t, 0, broadcaster.calls)
}

func prepareTestMessageChunk(t *testing.T, ctx context.Context, reservationService privacyreservation.Service) MessageChunk {
	t.Helper()

	planner := Service{Reservation: reservationService, Now: testNow}
	input := testPayrollInput()
	input.Items = []PayrollItemInput{
		{ItemID: "item-1", EmployeeID: "employee-1", RecipientAddress: testRecipientAddress("1"), Amount: big.NewInt(70)},
		{ItemID: "item-2", EmployeeID: "employee-2", RecipientAddress: testRecipientAddress("2"), Amount: big.NewInt(50)},
	}
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large-1", "uclair", 100, false, ""),
		testTreasuryNote("zero-1", "uclair", 0, false, ""),
		testTreasuryNote("large-2", "uclair", 80, false, ""),
		testTreasuryNote("zero-2", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	proofWorker := ProofWorker{
		Reservation:    reservationService,
		PayloadBuilder: fakePayloadBuilder{},
		ProofRunner:    fakeProofRunner{},
		Assembler:      fakeAssembler{},
		LeaseOwner:     "proof-worker-a",
		LeaseTTL:       time.Minute,
	}
	results := make([]ProofResult, 0, len(confirmed.Items))
	for _, item := range confirmed.Items {
		result, err := proofWorker.Process(ctx, item)
		require.NoError(t, err)
		result.Message = &privacytypes.MsgTransfer{Nullifiers: [][]byte{[]byte(item.OperationID + "-a"), []byte(item.OperationID + "-b")}}
		results = append(results, *result)
	}
	chunks, err := ChunkProofResults(results, ChunkOptions{MaxMessagesPerTx: 10})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	return chunks[0]
}

func testProofResult(operationID string, nullifierPrefix string) ProofResult {
	return ProofResult{
		Item: PayrollPlanItem{OperationID: operationID},
		Message: &privacytypes.MsgTransfer{
			Nullifiers: [][]byte{[]byte(nullifierPrefix + "-a"), []byte(nullifierPrefix + "-b")},
		},
	}
}

type recordingBroadcaster struct {
	calls         int
	messageCounts []int
}

func (b *recordingBroadcaster) BroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*BroadcastResult, error) {
	b.calls++
	b.messageCounts = append(b.messageCounts, len(msgs))
	return &BroadcastResult{
		TxHash:          "TXHASH",
		TxBytesHash:     "tx-bytes-hash",
		SignDocHash:     "sign-doc-hash",
		AccountSequence: 7,
		Height:          11,
	}, nil
}
