package payroll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
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

func TestChunkProofResultsRejectsDuplicateNullifiersWithinOperation(t *testing.T) {
	result := testProofResult("op-1", "same")
	result.Message.Nullifiers[1] = append([]byte(nil), result.Message.Nullifiers[0]...)

	_, err := ChunkProofResults([]ProofResult{result}, ChunkOptions{MaxMessagesPerTx: 1})
	require.ErrorContains(t, err, "nullifier index 1 duplicates index 0")
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
		results = append(results, *result)
	}
	chunks, err := ChunkProofResults(results, ChunkOptions{MaxMessagesPerTx: 10})
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster}
	result, err := worker.SubmitChunk(ctx, chunks[0])
	require.NoError(t, err)
	require.Equal(t, "TXHASH", result.TxHash)
	require.Equal(t, 1, broadcaster.calls)
	require.Len(t, broadcaster.messageCounts, 1)
	require.Equal(t, 2, broadcaster.messageCounts[0])
}

func TestBatchBroadcastWorkerFallsBackToUnknownWhenSubmittedWriteFails(t *testing.T) {
	ctx := context.Background()
	store := &submittedWriteFailingStore{
		MemoryStore: privacyreservation.NewMemoryStore(),
		failOnce:    true,
	}
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{
		Assembler:   fakeAssembler{},
		Reservation: reservationService,
		Broadcaster: broadcaster,
	}

	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "submitted write failed")
	require.Equal(t, 1, broadcaster.calls)
	for _, note := range chunk.Results[0].Item.InputNotes {
		stored, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusUnknown, stored.Status)
		require.Equal(t, "TXHASH", stored.TxHash)
	}

	_, retryErr := worker.SubmitChunk(ctx, chunk)
	require.Error(t, retryErr)
	require.Equal(t, 1, broadcaster.calls)
}

func TestBatchBroadcastWorkerRejectsExpiredLeaseBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	now = now.Add(2 * time.Minute)
	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster, LeaseTTL: time.Minute}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, privacyreservation.ErrLeaseUnavailable)
	require.Equal(t, 0, broadcaster.calls)
}

func TestBatchBroadcastWorkerKeepsHeartbeatUntilReservationBookkeepingCompletes(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		broadcaster MessageBroadcaster
		status      privacyreservation.ReservationStatus
		wantError   bool
	}{
		{name: "submitted", broadcaster: &recordingBroadcaster{}, status: privacyreservation.StatusSubmitted},
		{name: "unknown", broadcaster: codeErrorBroadcaster{}, status: privacyreservation.StatusUnknown, wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			store := &delayedBroadcastBookkeepingStore{
				MemoryStore: privacyreservation.NewMemoryStore(),
				delay:       50 * time.Millisecond,
			}
			now := func() time.Time { return time.Now().UTC() }
			reservationService := privacyreservation.Service{Store: store, Now: now}
			chunk, reservationID := proofReadyChunkWithLiveLease(t, ctx, reservationService)

			worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
				Reservation: reservationService,
				Broadcaster: testCase.broadcaster,
				LeaseTTL:    20 * time.Millisecond,
			}
			_, err := worker.SubmitChunk(ctx, chunk)
			if testCase.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			reservation, err := store.GetReservation(ctx, reservationID)
			require.NoError(t, err)
			require.Equal(t, testCase.status, reservation.Status)
		})
	}
}

func TestBatchBroadcastWorkerRejectsUnpreparedBroadcasterBeforeBoundary(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	firstWorker := BatchBroadcastWorker{Assembler: fakeAssembler{},
		Reservation: reservationService,
		Broadcaster: noMetadataErrorBroadcaster{err: errors.New("rpc connection reset")},
		LeaseOwner:  "broadcast-worker-a",
		LeaseTTL:    time.Minute,
	}
	_, err := firstWorker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, ErrPreparedBroadcastUnsupported)
	for _, result := range chunk.Results {
		for _, note := range result.Item.InputNotes {
			reservation, err := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, err)
			require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
			require.NotEmpty(t, reservation.LeaseToken)
			require.Zero(t, reservation.BroadcastAttemptCount)
		}
	}
}

func TestBatchBroadcastWorkerUsesStoredIdentityAfterOpaqueRPCError(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
	chunk, reservationID := proofReadyChunkWithLeaseTTLAndIdentity(t, ctx, reservationService, time.Minute, "0XCAFE")

	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
		Reservation: reservationService,
		Broadcaster: preparedRPCErrorBroadcaster{
			identity: BroadcastResult{TxBytesHash: "0XCAFE"},
			err:      errors.New("rpc timeout"),
		},
		LeaseTTL:    time.Minute,
	}
	result, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "rpc timeout")
	require.NotNil(t, result)
	require.Equal(t, "0XCAFE", result.TxBytesHash)

	stored, err := store.GetReservation(ctx, reservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusUnknown, stored.Status)
	require.Equal(t, "0XCAFE", stored.TxBytesHash)
	require.Empty(t, stored.LeaseToken)
}

func TestBatchBroadcastWorkerReconcilesAcceptedTxAfterRPCResponseLoss(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
	chunk, reservationID := proofReadyChunkWithLeaseTTL(t, ctx, reservationService, time.Minute)

	worker := BatchBroadcastWorker{
		Assembler:   fakeAssembler{},
		Reservation: reservationService,
		Broadcaster: preparedRPCErrorBroadcaster{
			identity: BroadcastResult{
				TxHash:          "ACCEPTED_TX_HASH",
				TxBytesHash:     "accepted-tx-bytes-hash",
				SignDocHash:     "accepted-sign-doc-hash",
				AccountSequence: 19,
			},
			err: errors.New("rpc response lost after node acceptance"),
		},
		LeaseTTL: time.Minute,
	}
	result, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "rpc response lost")
	require.Equal(t, "ACCEPTED_TX_HASH", result.TxHash)

	stored, err := store.GetReservation(ctx, reservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusUnknown, stored.Status)
	require.Equal(t, "ACCEPTED_TX_HASH", stored.TxHash)

	reconciled, err := reservationService.Reconcile(ctx, reservationID, privacyreservation.OperationEvidence{
		TxHash:              "accepted_tx_hash",
		TxSucceeded:         true,
		NullifierSpent:      true,
		OutputCommitment:    "commitment-live",
		DisclosureDigest:    "digest-live",
		RecipientHash:       "recipient-live",
		AmountHash:          "amount-live",
		Denom:               "uclair",
		BatchItemIndex:      0,
		BatchItemIndexKnown: true,
	})
	require.NoError(t, err)
	require.False(t, reconciled.RequiresReview)
	require.Equal(t, privacyreservation.StatusConfirmedSpent, reconciled.ReservationStatus)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, reconciled.OperationStatus)
}

func TestBatchBroadcastWorkerTreatsNilPreparedResultAsAmbiguous(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	worker := BatchBroadcastWorker{
		Assembler:   fakeAssembler{},
		Reservation: reservationService,
		Broadcaster: nilPreparedResultBroadcaster{},
	}

	result, err := worker.SubmitChunk(ctx, chunk)
	require.Nil(t, result)
	require.ErrorContains(t, err, "message broadcaster returned nil result")
	for _, proofResult := range chunk.Results {
		operation, getErr := store.GetOperation(ctx, proofResult.Item.OperationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.OperationStatusManualReview, operation.Status)
		for _, note := range proofResult.Item.InputNotes {
			reservation, getErr := store.GetReservation(ctx, note.ReservationID)
			require.NoError(t, getErr)
			require.Equal(t, privacyreservation.StatusManualReview, reservation.Status)
		}
	}
}

func TestBatchBroadcastWorkerRejectsPreparedBroadcastWithoutIdentity(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		broadcaster MessageBroadcaster
		wantError   string
	}{
		{
			name: "successful code zero",
			broadcaster: postBoundaryResultBroadcaster{
				result: &BroadcastResult{},
			},
			wantError: "prepared broadcaster returned no durable tx identity",
		},
		{
			name: "nonzero code",
			broadcaster: postBoundaryResultBroadcaster{
				result: &BroadcastResult{Code: 17, RawLog: "out of gas"},
			},
			wantError: "prepared broadcaster returned no durable tx identity",
		},
		{
			name: "result with rpc error",
			broadcaster: postBoundaryResultBroadcaster{
				result: &BroadcastResult{},
				err:    errors.New("rpc response lost"),
			},
			wantError: "prepared broadcaster returned no durable tx identity",
		},
		{
			name: "sign doc only",
			broadcaster: postBoundaryResultBroadcaster{
				result: &BroadcastResult{SignDocHash: "pre-broadcast-sign-doc"},
				err:    errors.New("rpc response lost"),
			},
			wantError: "prepared broadcaster returned no durable tx identity",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			store := privacyreservation.NewMemoryStore()
			reservationService := privacyreservation.Service{Store: store, Now: testNow}
			chunk := prepareTestMessageChunk(t, ctx, reservationService)
			worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: testCase.broadcaster}

			_, err := worker.SubmitChunk(ctx, chunk)
			require.ErrorContains(t, err, testCase.wantError)
			for _, result := range chunk.Results {
				for _, note := range result.Item.InputNotes {
					reservation, getErr := store.GetReservation(ctx, note.ReservationID)
					require.NoError(t, getErr)
					require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
					require.Zero(t, reservation.BroadcastAttemptCount)
				}
			}
		})
	}
}

func TestBatchBroadcastWorkerHeartbeatsDuringNullifierPreflight(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
	chunk, reservationID := proofReadyChunkWithLeaseTTL(t, ctx, reservationService, 100*time.Millisecond)
	checker := &blockingNullifierChecker{started: make(chan struct{}), release: make(chan struct{})}
	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
		Reservation:      reservationService,
		Broadcaster:      broadcaster,
		NullifierChecker: checker,
		LeaseTTL:         100 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() {
		_, err := worker.SubmitChunk(ctx, chunk)
		done <- err
	}()
	<-checker.started
	time.Sleep(300 * time.Millisecond)
	_, err := reservationService.AcquireLeaseForStatus(ctx, reservationID, "broadcast-worker-b", privacyreservation.StatusProofReady, 100*time.Millisecond)
	require.ErrorIs(t, err, privacyreservation.ErrLeaseUnavailable)
	close(checker.release)
	require.NoError(t, <-done)
	require.Equal(t, 1, broadcaster.calls)
}

func TestSubmissionLeaseCommitCancelsInFlightHeartbeat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &blockingSubmissionHeartbeatStore{
		MemoryStore: privacyreservation.NewMemoryStore(),
		started:     make(chan struct{}),
	}
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
	done := make(chan error, 1)

	go func() {
		_, err := broadcastWithSubmissionLeaseHeartbeat(
			ctx,
			reservationService,
			[]privacyreservation.SubmittedReservationRef{{
				ReservationID: "reservation-a",
				LeaseOwner:    "broadcast-worker-a",
				LeaseToken:    "lease-token-a",
			}},
			20*time.Millisecond,
			func(_ context.Context, commit submissionLeaseCommit, _ submissionLeaseRefresh) (*BroadcastResult, error) {
				<-store.started
				if err := commit(func(context.Context, error) error { return nil }); err != nil {
					return nil, err
				}
				return &BroadcastResult{TxHash: "TXHASH"}, nil
			},
		)
		done <- err
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("terminal commit did not cancel the in-flight scheduler heartbeat")
	}
}

func TestSubmissionTerminalLeaseTTLIncludesHeartbeatAndCommitBudgets(t *testing.T) {
	minimum := submissionFinalHeartbeatTimeout + submissionTerminalCommitTimeout + submissionTerminalLeaseMargin
	require.Equal(t, minimum, submissionTerminalLeaseTTL(time.Second))
	require.Equal(t, 2*minimum, submissionTerminalLeaseTTL(2*minimum))
}

func TestBatchBroadcastWorkerCommitsSuccessAfterParentContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &contextAwareSubmittedStore{MemoryStore: privacyreservation.NewMemoryStore()}
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
	chunk, reservationID := proofReadyChunkWithLiveLease(t, ctx, reservationService)
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
		Reservation: reservationService,
		Broadcaster: cancelingSuccessBroadcaster{cancel: cancel},
		LeaseTTL:    20 * time.Millisecond,
	}

	result, err := worker.SubmitChunk(ctx, chunk)
	require.NoError(t, err)
	require.Equal(t, "TXHASH", result.TxHash)
	require.True(t, store.submittedWithLiveContext)
	reservation, err := store.GetReservation(context.Background(), reservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusSubmitted, reservation.Status)
}

func TestBatchBroadcastWorkerUsesFreshContextAfterFinalHeartbeatTimeout(t *testing.T) {
	ctx := context.Background()
	afterBroadcast := &atomic.Bool{}
	store := &finalHeartbeatTimeoutStore{
		MemoryStore:    privacyreservation.NewMemoryStore(),
		afterBroadcast: afterBroadcast,
	}
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
	chunk, reservationID := proofReadyChunkWithLiveLease(t, ctx, reservationService)
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
		Reservation: reservationService,
		Broadcaster: finalHeartbeatTimeoutBroadcaster{afterBroadcast: afterBroadcast},
		LeaseTTL:    time.Hour,
	}

	result, err := worker.SubmitChunk(ctx, chunk)
	require.NoError(t, err)
	require.Equal(t, "TXHASH", result.TxHash)
	require.True(t, store.submittedWithLiveContext)
	reservation, err := store.GetReservation(ctx, reservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusSubmitted, reservation.Status)
}

func TestBatchBroadcastWorkerHeartbeatsProofReadyLeaseDuringBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	broadcaster := &delayedBroadcaster{delay: 250 * time.Millisecond}
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
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

func TestBatchBroadcastWorkerPersistsTerminalStateAfterPostBroadcastHeartbeatFailure(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		broadcaster MessageBroadcaster
		status      privacyreservation.ReservationStatus
		wantError   string
	}{
		{
			name: "submitted",
			broadcaster: postBoundaryResultBroadcaster{
				delay:  60 * time.Millisecond,
				result: &BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash"},
			},
			status: privacyreservation.StatusSubmitted,
		},
		{
			name: "unknown",
			broadcaster: postBoundaryResultBroadcaster{
				delay:  60 * time.Millisecond,
				result: &BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash"},
				err:    errors.New("rpc response lost"),
			},
			status:    privacyreservation.StatusUnknown,
			wantError: "rpc response lost",
		},
		{
			name: "ambiguous",
			broadcaster: postBoundaryResultBroadcaster{
				delay: 60 * time.Millisecond,
			},
			status:    privacyreservation.StatusManualReview,
			wantError: "nil result",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			store := &postBroadcastHeartbeatFailingStore{
				MemoryStore: privacyreservation.NewMemoryStore(),
				failAtCall:  3,
			}
			reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
			chunk, reservationID := proofReadyChunkWithLiveLease(t, ctx, reservationService)
			worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
				Reservation: reservationService,
				Broadcaster: testCase.broadcaster,
				LeaseTTL:    100 * time.Millisecond,
			}

			_, err := worker.SubmitChunk(ctx, chunk)
			if testCase.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, testCase.wantError)
			}
			require.GreaterOrEqual(t, store.heartbeatCalls, 4)

			reservation, err := store.GetReservation(ctx, reservationID)
			require.NoError(t, err)
			require.Equal(t, testCase.status, reservation.Status)
			require.Contains(t, reservation.LastBroadcastError, "injected heartbeat failure")
		})
	}
}

func TestBatchBroadcastWorkerPersistsSubmittedAfterFinalHeartbeatFailure(t *testing.T) {
	ctx := context.Background()
	store := &postBroadcastHeartbeatFailingStore{
		MemoryStore: privacyreservation.NewMemoryStore(),
		failAtCall:  3,
	}
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return time.Now().UTC() }}
	chunk, reservationID := proofReadyChunkWithLiveLease(t, ctx, reservationService)
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
		Reservation: reservationService,
		Broadcaster: &recordingBroadcaster{},
		LeaseTTL:    time.Second,
	}

	result, err := worker.SubmitChunk(ctx, chunk)
	require.NoError(t, err)
	require.Equal(t, "TXHASH", result.TxHash)
	require.Equal(t, 3, store.heartbeatCalls)
	reservation, err := store.GetReservation(ctx, reservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusSubmitted, reservation.Status)
	require.Contains(t, reservation.LastBroadcastError, "injected heartbeat failure")
}

func TestBatchBroadcastWorkerDoesNotPartiallySubmitChunkOnOperationFailure(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	chunk.Results[1].Item.OperationID = "missing-operation"

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster}
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
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster}
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
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster}
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
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorIs(t, err, privacyreservation.ErrInvalidReservation)
	require.Equal(t, 0, broadcaster.calls)
}

func TestBatchBroadcastWorkerRejectsMessageMutationBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	chunk.Results[1].Message.Nullifiers[0] = append([]byte(nil), chunk.Results[0].Message.Nullifiers[0]...)

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "message does not match payload and proof")
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
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster}
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
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
		Reservation:      reservationService,
		Broadcaster:      broadcaster,
		NullifierChecker: checker,
	}
	_, err := worker.SubmitChunk(ctx, chunk)
	require.ErrorContains(t, err, "already spent")
	var spentErr SpentNullifierError
	require.ErrorAs(t, err, &spentErr)
	require.NotContains(t, err.Error(), spentNullifier)
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

func TestBatchBroadcastWorkerRejectsExpiredProofReadyLeaseWithoutTakeover(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	require.GreaterOrEqual(t, len(chunk.Results[0].Item.InputNotes), 2)
	firstReservationID := chunk.Results[0].Item.InputNotes[0].ReservationID
	before, err := store.GetReservation(ctx, firstReservationID)
	require.NoError(t, err)

	now = now.Add(2 * time.Minute)

	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
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
	require.Equal(t, before.LeaseOwner, firstReservation.LeaseOwner)
	require.Equal(t, before.LeaseToken, firstReservation.LeaseToken)
	require.Equal(t, before.LeaseUntil, firstReservation.LeaseUntil)
}

func TestBatchBroadcastWorkerRecordsUnknownAttemptOnBroadcastErrorWithMetadata(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)

	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
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

	worker := BatchBroadcastWorker{Assembler: fakeAssembler{},
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
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster}

	_, err := worker.SubmitChunk(ctx, MessageChunk{
		ChunkID: "chunk-1",
		Results: []ProofResult{{
			Item:              PayrollPlanItem{OperationID: "op-a"},
			Message:           &privacytypes.MsgTransfer{},
			Payload:           privacytransfer.PreparedTransferPayload{PayloadHash: "payload-a"},
			Proof:             privacytransfer.PreparedTransferProof{PayloadHash: "payload-a"},
			ReservationLeases: map[string]string{},
		}},
	})
	require.ErrorContains(t, err, "has no input notes")
	require.Equal(t, 0, broadcaster.calls)
}

func TestValidateProofResultArtifactRejectsEmptyProof(t *testing.T) {
	payload := privacytransfer.PreparedTransferPayload{
		Version: privacytransfer.PreparedTransferPayloadVersion,
		Inputs:  []privacytransfer.PreparedTransferInput{{NullifierHex: testCanonicalNullifierHex("empty-proof-nullifier")}},
	}
	payload.PayloadHash = privacytransfer.ComputePreparedTransferPayloadHash(payload)
	proof := privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "",
	}
	message, err := fakeAssembler{}.BuildTransferMessage(payload, proof)
	require.NoError(t, err)

	err = validateProofResultArtifact(ProofResult{Payload: payload, Proof: proof, Message: message}, fakeAssembler{})
	require.ErrorContains(t, err, "empty proof hex")
}

func TestBatchBroadcastWorkerRejectsChunkWithoutResults(t *testing.T) {
	ctx := context.Background()
	reservationService := privacyreservation.Service{Store: privacyreservation.NewMemoryStore(), Now: testNow}
	broadcaster := &recordingBroadcaster{}
	worker := BatchBroadcastWorker{Assembler: fakeAssembler{}, Reservation: reservationService, Broadcaster: broadcaster}

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
		results = append(results, *result)
	}
	chunks, err := ChunkProofResults(results, ChunkOptions{MaxMessagesPerTx: 10})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	return chunks[0]
}

func proofReadyChunkWithLiveLease(t *testing.T, ctx context.Context, reservationService privacyreservation.Service) (MessageChunk, string) {
	return proofReadyChunkWithLeaseTTL(t, ctx, reservationService, time.Second)
}

func proofReadyChunkWithLeaseTTL(t *testing.T, ctx context.Context, reservationService privacyreservation.Service, leaseTTL time.Duration) (MessageChunk, string) {
	return proofReadyChunkWithLeaseTTLAndIdentity(t, ctx, reservationService, leaseTTL, "")
}

func proofReadyChunkWithLeaseTTLAndIdentity(t *testing.T, ctx context.Context, reservationService privacyreservation.Service, leaseTTL time.Duration, txBytesHash string) (MessageChunk, string) {
	t.Helper()
	const reservationID = "r-live"
	const operationID = "op-live"
	created, err := reservationService.Reserve(ctx, privacyreservation.ReserveInput{
		Reservation: privacyreservation.NoteReservation{
			ReservationID:      reservationID,
			NoteID:             "note-live",
			OwnerKeyID:         "owner-live",
			NullifierLookupKey: "lookup-live",
			OperationID:        operationID,
			Status:             privacyreservation.StatusReserved,
		},
		Operation: &privacyreservation.PayrollOperation{
			OperationID:              operationID,
			ExpectedOutputCommitment: "commitment-live",
			ExpectedDisclosureDigest: "digest-live",
			ExpectedRecipientHash:    "recipient-live",
			ExpectedAmountHash:       "amount-live",
			ExpectedDenom:            "uclair",
			BatchItemIndex:           0,
			BatchItemIndexKnown:      true,
			Status:                   privacyreservation.OperationStatusPlanned,
		},
	})
	require.NoError(t, err)
	lease, err := reservationService.AcquireLease(ctx, created.ReservationID, "broadcast-worker-a", leaseTTL)
	require.NoError(t, err)
	_, err = reservationService.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving)
	require.NoError(t, err)
	payload := privacytransfer.PreparedTransferPayload{
		Version: privacytransfer.PreparedTransferPayloadVersion,
		Inputs:  []privacytransfer.PreparedTransferInput{{NullifierHex: testCanonicalNullifierHex("live-nullifier")}},
	}
	payload.PayloadHash = privacytransfer.ComputePreparedTransferPayloadHash(payload)
	proof := privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "aa",
	}
	message, err := fakeAssembler{}.BuildTransferMessage(payload, proof)
	require.NoError(t, err)
	_, _, err = reservationService.MarkProofReadyBatch(ctx, []privacyreservation.SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, privacyreservation.ProofReadyOperationUpdate{
		OperationID:              operationID,
		PayloadHash:              payload.PayloadHash,
		TxBytesHash:              txBytesHash,
		ExpectedOutputCommitment: "commitment-live",
		ExpectedDisclosureDigest: "digest-live",
	})
	require.NoError(t, err)

	return MessageChunk{
		ChunkID: "live-lease",
		Results: []ProofResult{{
			Item: PayrollPlanItem{
				OperationID: operationID,
				InputNotes:  []TreasuryNote{{ReservationID: reservationID}},
			},
			Message:                message,
			Payload:                payload,
			Proof:                  proof,
			ReservationLeases:      map[string]string{reservationID: lease.Token},
			ReservationLeaseOwners: map[string]string{reservationID: lease.Owner},
		}},
	}, reservationID
}

func testProofResult(operationID string, nullifierPrefix string) ProofResult {
	return ProofResult{
		Item: PayrollPlanItem{OperationID: operationID},
		Message: &privacytypes.MsgTransfer{
			Nullifiers: [][]byte{testCanonicalNullifier(nullifierPrefix + "-a"), testCanonicalNullifier(nullifierPrefix + "-b")},
		},
	}
}

func testCanonicalNullifier(label string) []byte {
	sum := sha256.Sum256([]byte(label))
	// Keep the fixture below the BN254 scalar modulus without reducing it.
	sum[0] &= 0x1f
	return append([]byte(nil), sum[:]...)
}

func testCanonicalNullifierHex(label string) string {
	return hex.EncodeToString(testCanonicalNullifier(label))
}

type recordingBroadcaster struct {
	calls         int
	messageCounts []int
}

type delayedBroadcastBookkeepingStore struct {
	*privacyreservation.MemoryStore
	delay time.Duration
}

type submittedWriteFailingStore struct {
	*privacyreservation.MemoryStore
	failOnce bool
}

func (s *submittedWriteFailingStore) MarkReservationsSubmitted(ctx context.Context, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, update privacyreservation.SubmittedReservationUpdate, now time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	if s.failOnce {
		s.failOnce = false
		return nil, nil, errors.New("submitted write failed")
	}
	return s.MemoryStore.MarkReservationsSubmitted(ctx, refs, operationIDs, update, now)
}

type contextAwareSubmittedStore struct {
	*privacyreservation.MemoryStore
	submittedWithLiveContext bool
}

type finalHeartbeatTimeoutStore struct {
	*privacyreservation.MemoryStore
	afterBroadcast           *atomic.Bool
	submittedWithLiveContext bool
}

func (s *finalHeartbeatTimeoutStore) HeartbeatReservationLeaseForStatus(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, requiredStatus privacyreservation.ReservationStatus, leaseUntil time.Time, now time.Time) (*privacyreservation.NoteReservation, error) {
	if s.afterBroadcast.Load() {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.MemoryStore.HeartbeatReservationLeaseForStatus(ctx, reservationID, leaseOwner, leaseToken, requiredStatus, leaseUntil, now)
}

func (s *finalHeartbeatTimeoutStore) MarkReservationsSubmitted(ctx context.Context, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, update privacyreservation.SubmittedReservationUpdate, now time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.submittedWithLiveContext = true
	return s.MemoryStore.MarkReservationsSubmitted(ctx, refs, operationIDs, update, now)
}

func (s *contextAwareSubmittedStore) MarkReservationsSubmitted(ctx context.Context, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, update privacyreservation.SubmittedReservationUpdate, now time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.submittedWithLiveContext = true
	return s.MemoryStore.MarkReservationsSubmitted(ctx, refs, operationIDs, update, now)
}

func (s *delayedBroadcastBookkeepingStore) MarkReservationsSubmitted(ctx context.Context, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, update privacyreservation.SubmittedReservationUpdate, _ time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	time.Sleep(s.delay)
	return s.MemoryStore.MarkReservationsSubmitted(ctx, refs, operationIDs, update, time.Now().UTC())
}

func (s *delayedBroadcastBookkeepingStore) MarkReservationsBroadcastUnknown(ctx context.Context, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, update privacyreservation.BroadcastAttemptUpdate, _ time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	time.Sleep(s.delay)
	return s.MemoryStore.MarkReservationsBroadcastUnknown(ctx, refs, operationIDs, update, time.Now().UTC())
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

func (b *recordingBroadcaster) PrepareBroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	return &PreparedMessageBroadcast{
		Identity: BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash", AccountSequence: 7},
		Submit: func(ctx context.Context) (*BroadcastResult, error) {
			return b.BroadcastMessages(ctx, msgs...)
		},
	}, nil
}

func (b *recordingBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type finalHeartbeatTimeoutBroadcaster struct {
	afterBroadcast *atomic.Bool
}

func (b finalHeartbeatTimeoutBroadcaster) BroadcastMessages(_ context.Context, _ ...sdk.Msg) (*BroadcastResult, error) {
	b.afterBroadcast.Store(true)
	return &BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash"}, nil
}

func (b finalHeartbeatTimeoutBroadcaster) PrepareBroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	return &PreparedMessageBroadcast{
		Identity: BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash"},
		Submit: func(ctx context.Context) (*BroadcastResult, error) {
			return b.BroadcastMessages(ctx, msgs...)
		},
	}, nil
}

func (b finalHeartbeatTimeoutBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type delayedBroadcaster struct {
	delay time.Duration
	calls int
}

// postBoundaryResultBroadcaster intentionally ignores context cancellation.
// It models a transport that has crossed the external broadcast boundary when
// its local lease heartbeat later fails.
type postBoundaryResultBroadcaster struct {
	delay  time.Duration
	result *BroadcastResult
	err    error
}

func (b postBoundaryResultBroadcaster) BroadcastMessages(_ context.Context, _ ...sdk.Msg) (*BroadcastResult, error) {
	time.Sleep(b.delay)
	return b.result, b.err
}

func (b postBoundaryResultBroadcaster) PrepareBroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	identity := BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash"}
	if b.result != nil {
		identity = *b.result
	}
	return &PreparedMessageBroadcast{
		Identity: identity,
		Submit: func(ctx context.Context) (*BroadcastResult, error) {
			return b.BroadcastMessages(ctx, msgs...)
		},
	}, nil
}

func (b postBoundaryResultBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type postBroadcastHeartbeatFailingStore struct {
	*privacyreservation.MemoryStore
	failAtCall     int
	heartbeatCalls int
}

func (s *postBroadcastHeartbeatFailingStore) HeartbeatReservationLeaseForStatus(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, requiredStatus privacyreservation.ReservationStatus, leaseUntil time.Time, now time.Time) (*privacyreservation.NoteReservation, error) {
	s.heartbeatCalls++
	if s.heartbeatCalls == s.failAtCall {
		return nil, errors.New("injected heartbeat failure")
	}
	return s.MemoryStore.HeartbeatReservationLeaseForStatus(ctx, reservationID, leaseOwner, leaseToken, requiredStatus, leaseUntil, now)
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

func (b *delayedBroadcaster) PrepareBroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	return &PreparedMessageBroadcast{
		Identity: BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash", AccountSequence: 7},
		Submit: func(ctx context.Context) (*BroadcastResult, error) {
			return b.BroadcastMessages(ctx, msgs...)
		},
	}, nil
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

func (b metadataErrorBroadcaster) PrepareBroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	return &PreparedMessageBroadcast{
		Identity: BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash", AccountSequence: 7},
		Submit: func(ctx context.Context) (*BroadcastResult, error) {
			return b.BroadcastMessages(ctx, msgs...)
		},
	}, nil
}

func (b metadataErrorBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type noMetadataErrorBroadcaster struct {
	err error
}

type preparedRPCErrorBroadcaster struct {
	identity BroadcastResult
	err      error
}

func (b preparedRPCErrorBroadcaster) BroadcastMessages(context.Context, ...sdk.Msg) (*BroadcastResult, error) {
	return nil, b.err
}

func (b preparedRPCErrorBroadcaster) PrepareBroadcastMessages(context.Context, ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	return &PreparedMessageBroadcast{
		Identity: b.identity,
		Submit: func(context.Context) (*BroadcastResult, error) {
			return nil, b.err
		},
	}, nil
}

func (b preparedRPCErrorBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

func (b noMetadataErrorBroadcaster) BroadcastMessages(_ context.Context, _ ...sdk.Msg) (*BroadcastResult, error) {
	return nil, b.err
}

func (b noMetadataErrorBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type nilPreparedResultBroadcaster struct{}

func (nilPreparedResultBroadcaster) BroadcastMessages(context.Context, ...sdk.Msg) (*BroadcastResult, error) {
	return nil, nil
}

func (nilPreparedResultBroadcaster) PrepareBroadcastMessages(context.Context, ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	return &PreparedMessageBroadcast{
		Identity: BroadcastResult{
			TxBytesHash:     "tx-bytes-hash",
			SignDocHash:     "sign-doc-hash",
			AccountSequence: 7,
		},
		Submit: func(context.Context) (*BroadcastResult, error) {
			return nil, nil
		},
	}, nil
}

func (nilPreparedResultBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type blockingSubmissionHeartbeatStore struct {
	*privacyreservation.MemoryStore
	started chan struct{}
	calls   atomic.Int32
}

func (s *blockingSubmissionHeartbeatStore) HeartbeatReservationLeaseForStatus(ctx context.Context, reservationID string, _ string, _ string, requiredStatus privacyreservation.ReservationStatus, _ time.Time, _ time.Time) (*privacyreservation.NoteReservation, error) {
	if s.calls.Add(1) == 1 {
		close(s.started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &privacyreservation.NoteReservation{
		ReservationID: reservationID,
		Status:        requiredStatus,
	}, nil
}

type blockingNullifierChecker struct {
	started chan struct{}
	release chan struct{}
}

func (c *blockingNullifierChecker) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	close(c.started)
	<-c.release
	return allNullifiersUnspent(nullifierHexes), nil
}

type cancelingSuccessBroadcaster struct {
	cancel context.CancelFunc
}

func (b cancelingSuccessBroadcaster) BroadcastMessages(_ context.Context, _ ...sdk.Msg) (*BroadcastResult, error) {
	b.cancel()
	return &BroadcastResult{
		TxHash:          "TXHASH",
		TxBytesHash:     "tx-bytes-hash",
		SignDocHash:     "sign-doc-hash",
		AccountSequence: 7,
	}, nil
}

func (b cancelingSuccessBroadcaster) PrepareBroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	return &PreparedMessageBroadcast{
		Identity: BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash", AccountSequence: 7},
		Submit: func(ctx context.Context) (*BroadcastResult, error) {
			return b.BroadcastMessages(ctx, msgs...)
		},
	}, nil
}

func (b cancelingSuccessBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
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

func (codeErrorBroadcaster) PrepareBroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	return &PreparedMessageBroadcast{
		Identity: BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash", AccountSequence: 7},
		Submit: func(ctx context.Context) (*BroadcastResult, error) {
			return codeErrorBroadcaster{}.BroadcastMessages(ctx, msgs...)
		},
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
