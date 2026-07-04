package payroll

import (
	"context"
	"encoding/hex"
	"errors"
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
	require.Len(t, chunks[0].Results, 2)
	require.Equal(t, "chunk-000002", chunks[1].ChunkID)
	require.Len(t, chunks[1].Results, 1)
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
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
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

func TestBatchBroadcastWorkerTakesOverExpiredProofReadyLease(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	now = now.Add(2 * time.Minute)
	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{
		Reservation: reservationService,
		Broadcaster: broadcaster,
		LeaseOwner:  "broadcast-worker-a",
		LeaseTTL:    time.Minute,
	}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.NoError(t, err)
	require.Equal(t, 1, broadcaster.calls)
	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusSubmitted, reservation.Status)
			require.Empty(t, reservation.LeaseOwner)
			require.Empty(t, reservation.LeaseToken)
			require.True(t, reservation.LeaseUntil.IsZero())
		}
	}
}

func TestBatchBroadcastWorkerRetriesWithStaleLeaseAfterTakeoverExpires(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	now = now.Add(2 * time.Minute)
	firstWorker := BatchBroadcastWorker{
		Reservation: reservationService,
		Broadcaster: noMetadataErrorBroadcaster{err: errors.New("rpc connection reset")},
		LeaseOwner:  "broadcast-worker-a",
		LeaseTTL:    time.Minute,
	}
	_, err := firstWorker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "rpc connection reset")
	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
			require.Equal(t, "broadcast-worker-a", reservation.LeaseOwner)
			require.NotEqual(t, result.ReservationLeases[note.ReservationID], reservation.LeaseToken)
		}
	}

	now = now.Add(2 * time.Minute)
	broadcaster := &recordingBroadcaster{}
	retryWorker := BatchBroadcastWorker{
		Reservation: reservationService,
		Broadcaster: broadcaster,
		LeaseOwner:  "broadcast-worker-b",
		LeaseTTL:    time.Minute,
	}
	_, err = retryWorker.SubmitChunk(ctx, chunk)
	require.NoError(t, err)
	require.Equal(t, 1, broadcaster.calls)
	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusSubmitted, reservation.Status)
			require.Empty(t, reservation.LeaseOwner)
			require.Empty(t, reservation.LeaseToken)
			require.True(t, reservation.LeaseUntil.IsZero())
		}
	}
}

func TestBatchBroadcastWorkerHeartbeatsProofReadyLeaseDuringBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	broadcaster := &delayedBroadcaster{delay: 250 * time.Millisecond}
	worker := BatchBroadcastWorker{
		Reservation: reservationService,
		Broadcaster: broadcaster,
		LeaseTTL:    100 * time.Millisecond,
	}
	result, err := worker.SubmitChunk(ctx, chunk)
	require.NoError(t, err)
	require.Equal(t, "TXHASH", result.TxHash)
	require.Equal(t, 1, broadcaster.calls)

	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusSubmitted, reservation.Status)
		}
	}
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

func TestBatchBroadcastWorkerRejectsCrossOperationReservationBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	chunk.Results[0].Item.OperationID = chunk.Results[1].Item.OperationID

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, privacyreservation.ErrInvalidReservation)
	require.Equal(t, 0, broadcaster.calls)
}

func TestBatchBroadcastWorkerRejectsDuplicateReservationRefBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	chunk.Results[0].Item.InputNotes = append(chunk.Results[0].Item.InputNotes, chunk.Results[0].Item.InputNotes[0])

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, privacyreservation.ErrInvalidReservation)
	require.Equal(t, 0, broadcaster.calls)
}

func TestBatchBroadcastWorkerRejectsMissingOperationReservationBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	require.GreaterOrEqual(t, len(chunk.Results[0].Item.InputNotes), 2)
	keptReservationID := chunk.Results[0].Item.InputNotes[0].ReservationID
	chunk.Results[0].Item.InputNotes = chunk.Results[0].Item.InputNotes[:1]
	for reservationID := range chunk.Results[0].ReservationLeases {
		if reservationID != keptReservationID {
			delete(chunk.Results[0].ReservationLeases, reservationID)
		}
	}

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, privacyreservation.ErrInvalidReservation)
	require.Equal(t, 0, broadcaster.calls)
}

func TestBatchBroadcastWorkerRejectsDuplicateNullifiersBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	chunk.Results[1].Message.Nullifiers[0] = append([]byte(nil), chunk.Results[0].Message.Nullifiers[0]...)

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "duplicate nullifier")
	require.Equal(t, 0, broadcaster.calls)

	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		}
	}
}

func TestBatchBroadcastWorkerRequiresNullifierCheckerBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	broadcaster := &uncheckedBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, ErrInvalidPayrollInput)
	require.Equal(t, 0, broadcaster.calls)

	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		}
	}
}

func TestBatchBroadcastWorkerRejectsSpentNullifierBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	spentNullifier := hex.EncodeToString(chunk.Results[0].Message.Nullifiers[0])

	broadcaster := &recordingBroadcaster{}
	checker := &stubBroadcastNullifierChecker{used: map[string]bool{spentNullifier: true}}
	worker := BatchBroadcastWorker{
		Reservation:      reservationService,
		Broadcaster:      broadcaster,
		NullifierChecker: checker,
	}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "already spent")
	var spentErr SpentNullifierError
	require.ErrorAs(t, err, &spentErr)
	require.Equal(t, spentNullifier, spentErr.NullifierHex)
	require.Equal(t, 0, broadcaster.calls)
	require.Len(t, checker.requests, 1)

	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		}
	}
}

func TestBatchBroadcastWorkerClearsTakeoverLeaseOnSpentNullifierBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	spentNullifier := hex.EncodeToString(chunk.Results[0].Message.Nullifiers[0])

	now = now.Add(2 * time.Minute)
	broadcaster := &recordingBroadcaster{}
	checker := &stubBroadcastNullifierChecker{used: map[string]bool{spentNullifier: true}}
	worker := BatchBroadcastWorker{
		Reservation:      reservationService,
		Broadcaster:      broadcaster,
		NullifierChecker: checker,
		LeaseOwner:       "broadcast-worker-a",
		LeaseTTL:         time.Minute,
	}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "already spent")
	require.Equal(t, 0, broadcaster.calls)

	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
			require.Empty(t, reservation.LeaseOwner)
			require.Empty(t, reservation.LeaseToken)
			require.True(t, reservation.LeaseUntil.IsZero())
		}
	}
}

func TestBatchBroadcastWorkerClearsPartialTakeoverLeaseOnLaterAcquireFailure(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	require.GreaterOrEqual(t, len(chunk.Results[0].Item.InputNotes), 2)
	firstReservationID := chunk.Results[0].Item.InputNotes[0].ReservationID
	secondReservationID := chunk.Results[0].Item.InputNotes[1].ReservationID

	now = now.Add(2 * time.Minute)
	otherLease, err := reservationService.AcquireLeaseForStatus(ctx, secondReservationID, "other-broadcast-worker", privacyreservation.StatusProofReady, time.Minute)
	require.NoError(t, err)

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{
		Reservation: reservationService,
		Broadcaster: broadcaster,
		LeaseOwner:  "broadcast-worker-a",
		LeaseTTL:    time.Minute,
	}
	_, err = worker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, privacyreservation.ErrLeaseUnavailable)
	require.Equal(t, 0, broadcaster.calls)

	firstReservation, err := store.GetReservation(ctx, firstReservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusProofReady, firstReservation.Status)
	require.Empty(t, firstReservation.LeaseOwner)
	require.Empty(t, firstReservation.LeaseToken)
	require.True(t, firstReservation.LeaseUntil.IsZero())

	secondReservation, err := store.GetReservation(ctx, secondReservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusProofReady, secondReservation.Status)
	require.Equal(t, "other-broadcast-worker", secondReservation.LeaseOwner)
	require.Equal(t, otherLease.Token, secondReservation.LeaseToken)
}

func TestBatchBroadcastWorkerRecordsUnknownAttemptOnBroadcastErrorWithMetadata(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	worker := BatchBroadcastWorker{
		Reservation: reservationService,
		Broadcaster: metadataErrorBroadcaster{err: errors.New("rpc timeout")},
	}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "rpc timeout")

	for _, result := range chunk.Results {
		operation, err := store.GetOperation(ctx, result.Item.OperationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.OperationStatusUnknown, operation.Status)
		require.Equal(t, "tx-bytes-hash", operation.TxBytesHash)
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusUnknown, reservation.Status)
			require.Equal(t, "rpc timeout", reservation.LastBroadcastError)
			require.Equal(t, 1, reservation.BroadcastAttemptCount)
		}
	}
}

func TestBatchBroadcastWorkerRecordsUnknownAttemptOnNonzeroCode(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	worker := BatchBroadcastWorker{
		Reservation: reservationService,
		Broadcaster: codeErrorBroadcaster{},
	}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "tx failed with code 17")

	for _, result := range chunk.Results {
		operation, err := store.GetOperation(ctx, result.Item.OperationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.OperationStatusUnknown, operation.Status)
		require.Equal(t, "TXHASH", operation.TxHash)
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusUnknown, reservation.Status)
			require.Equal(t, "tx failed with code 17: out of gas", reservation.LastBroadcastError)
			require.Equal(t, 1, reservation.BroadcastAttemptCount)
		}
	}
}

func TestBatchBroadcastWorkerRejectsProofResultWithoutInputNotes(t *testing.T) {
	ctx := context.Background()
	reservationService := privacyreservation.Service{Store: privacyreservation.NewMemoryStore(), Now: testNow}
	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}

	_, err := worker.SubmitChunk(ctx, MessageChunk{
		ChunkID: "chunk-1",
		Results: []ProofResult{{
			Item:              PayrollPlanItem{OperationID: "op-a"},
			Message:           &privacytypes.MsgTransfer{},
			ReservationLeases: map[string]string{},
		}},
	})
	require.ErrorContains(t, err, "has no input notes")
	require.Equal(t, 0, broadcaster.calls)
}

func TestBatchBroadcastWorkerRejectsChunkWithoutResults(t *testing.T) {
	ctx := context.Background()
	reservationService := privacyreservation.Service{Store: privacyreservation.NewMemoryStore(), Now: testNow}
	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Reservation: reservationService, Broadcaster: broadcaster}

	_, err := worker.SubmitChunk(ctx, MessageChunk{
		ChunkID: "chunk-1",
	})
	require.ErrorContains(t, err, "has no proof results")
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
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
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

func (b *recordingBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type delayedBroadcaster struct {
	delay time.Duration
	calls int
}

func (b *delayedBroadcaster) BroadcastMessages(ctx context.Context, _ ...sdk.Msg) (*BroadcastResult, error) {
	b.calls++
	select {
	case <-time.After(b.delay):
		return &BroadcastResult{
			TxHash:          "TXHASH",
			TxBytesHash:     "tx-bytes-hash",
			SignDocHash:     "sign-doc-hash",
			AccountSequence: 7,
			Height:          11,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *delayedBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type metadataErrorBroadcaster struct {
	err error
}

func (b metadataErrorBroadcaster) BroadcastMessages(_ context.Context, _ ...sdk.Msg) (*BroadcastResult, error) {
	return &BroadcastResult{
		TxHash:          "TXHASH",
		TxBytesHash:     "tx-bytes-hash",
		SignDocHash:     "sign-doc-hash",
		AccountSequence: 7,
	}, b.err
}

func (b metadataErrorBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type noMetadataErrorBroadcaster struct {
	err error
}

func (b noMetadataErrorBroadcaster) BroadcastMessages(_ context.Context, _ ...sdk.Msg) (*BroadcastResult, error) {
	return nil, b.err
}

func (b noMetadataErrorBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type codeErrorBroadcaster struct{}

func (codeErrorBroadcaster) BroadcastMessages(_ context.Context, _ ...sdk.Msg) (*BroadcastResult, error) {
	return &BroadcastResult{
		TxHash:          "TXHASH",
		TxBytesHash:     "tx-bytes-hash",
		SignDocHash:     "sign-doc-hash",
		AccountSequence: 7,
		Code:            17,
		RawLog:          "out of gas",
	}, nil
}

func (codeErrorBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type uncheckedBroadcaster struct {
	calls int
}

func (b *uncheckedBroadcaster) BroadcastMessages(_ context.Context, _ ...sdk.Msg) (*BroadcastResult, error) {
	b.calls++
	return &BroadcastResult{TxHash: "TXHASH"}, nil
}

type stubBroadcastNullifierChecker struct {
	used     map[string]bool
	requests [][]string
	err      error
}

func (c *stubBroadcastNullifierChecker) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	c.requests = append(c.requests, append([]string(nil), nullifierHexes...))
	if c.err != nil {
		return nil, c.err
	}
	out := make(map[string]bool, len(nullifierHexes))
	for _, nullifier := range nullifierHexes {
		out[nullifier] = c.used[nullifier]
	}
	return out, nil
}

func allNullifiersUnspent(nullifierHexes []string) map[string]bool {
	out := make(map[string]bool, len(nullifierHexes))
	for _, nullifier := range nullifierHexes {
		out[nullifier] = false
	}
	return out
}
