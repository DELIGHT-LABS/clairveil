package payroll

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestProofAndBroadcastWorkersAdvanceReservationLifecycle(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofStore := NewMemoryProofResultStore()
	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: proofStore,
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	proofResult, err := proofWorker.Process(ctx, item)
	require.NoError(t, err)
	require.NotNil(t, proofResult.Message)
	storedProofResult, err := proofStore.GetProofResult(ctx, item.OperationID)
	require.NoError(t, err)
	require.NotNil(t, storedProofResult.Message)
	require.Equal(t, proofResult.Proof.PayloadHash, storedProofResult.Proof.PayloadHash)

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		require.NotEmpty(t, reservation.LeaseToken)
	}
	operation, err := store.GetOperation(ctx, item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, operation.Status)
	require.Equal(t, "commitment-a", operation.ExpectedOutputCommitment)
	require.Equal(t, "audit-digest-a", operation.ExpectedDisclosureDigest)

	broadcastWorker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: fakeBroadcaster{},
		Assembler:   fakeAssembler{},
	}
	broadcastResult, err := broadcastWorker.SubmitProofResult(ctx, *proofResult)
	require.NoError(t, err)
	require.Equal(t, "TXHASH", broadcastResult.TxHash)

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusSubmitted, reservation.Status)
		require.Equal(t, "TXHASH", reservation.TxHash)
	}
	operation, err = store.GetOperation(ctx, item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSubmitted, operation.Status)
	require.Equal(t, "TXHASH", operation.TxHash)
}

func TestProofWorkerStoresAuditDisclosureDigestWhenMultiplePlanesExist(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation: reservationService,
		PayloadBuilder: multiPlanePayloadBuilder{
			payload: privacytransfer.PreparedTransferPayload{
				Version:                     privacytransfer.PreparedTransferPayloadVersion,
				PayloadHash:                 "payload-hash-a",
				UserDisclosureDigestHex:     "user-digest-a",
				AuditDisclosureDigestHex:    "audit-digest-a",
				SelfViewDisclosureDigestHex: "self-view-digest-a",
				Inputs: []privacytransfer.PreparedTransferInput{
					{NullifierHex: "nullifier-a"},
					{NullifierHex: "nullifier-b"},
				},
				Outputs: []privacytransfer.PreparedTransferOutput{
					{CommitmentHex: "commitment-a"},
					{CommitmentHex: "change-a"},
				},
			},
		},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	_, err = proofWorker.Process(ctx, item)
	require.NoError(t, err)

	operation, err := store.GetOperation(ctx, item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, operation.Status)
	require.Equal(t, "audit-digest-a", operation.ExpectedDisclosureDigest)
	require.Equal(t, "user-digest-a", operation.ExpectedUserDisclosureDigest)
	require.Equal(t, "audit-digest-a", operation.ExpectedAuditDisclosureDigest)
	require.Equal(t, "self-view-digest-a", operation.ExpectedSelfViewDisclosureDigest)
}

func TestBroadcastWorkerRecordsUnknownAttemptOnNonzeroCode(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	result := chunk.Results[0]

	broadcastWorker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: codeErrorBroadcaster{},
		Assembler:   fakeAssembler{},
	}
	_, err := broadcastWorker.SubmitProofResult(ctx, result)
	require.ErrorContains(t, err, "tx failed with code 17")

	operation, err := store.GetOperation(ctx, result.Item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusUnknown, operation.Status)
	require.Equal(t, "TXHASH", operation.TxHash)
	for _, note := range result.Item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusUnknown, reservation.Status)
		require.Equal(t, "tx failed with code 17: out of gas", reservation.LastBroadcastError)
	}
}

func TestBroadcastWorkerRejectsMutatedMessageBeforeBroadcast(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	result := chunk.Results[0]
	result.Message = &privacytypes.MsgTransfer{Nullifiers: [][]byte{[]byte("mutated")}}
	broadcaster := &recordingBroadcaster{}
	worker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: broadcaster,
		Assembler:   fakeAssembler{},
	}

	_, err := worker.SubmitProofResult(ctx, result)
	require.ErrorContains(t, err, "message does not match payload and proof")
	require.Equal(t, 0, broadcaster.calls)
}

func TestBroadcastWorkerFallsBackToManualReviewWhenTerminalWritesFail(t *testing.T) {
	ctx := context.Background()
	store := &terminalWriteFailingReservationStore{MemoryStore: privacyreservation.NewMemoryStore()}
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	result := chunk.Results[0]
	worker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: fakeBroadcaster{},
		Assembler:   fakeAssembler{},
	}

	_, err := worker.SubmitProofResult(ctx, result)
	require.ErrorContains(t, err, "submitted write failed")
	for _, note := range result.Item.InputNotes {
		stored, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusManualReview, stored.Status)
		require.Contains(t, stored.LastBroadcastError, `tx_bytes_hash="tx-bytes-hash"`)
	}
}

func TestBroadcastAttemptMarkerBlocksRetryWhenEveryTerminalWriteFails(t *testing.T) {
	ctx := context.Background()
	store := &allTerminalWritesFailingReservationStore{MemoryStore: privacyreservation.NewMemoryStore()}
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	result := chunk.Results[0]
	broadcaster := &recordingBroadcaster{}
	worker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: broadcaster,
		Assembler:   fakeAssembler{},
	}

	_, err := worker.SubmitProofResult(ctx, result)
	require.ErrorContains(t, err, "ambiguous write failed")
	require.Equal(t, 1, broadcaster.calls)
	for _, note := range result.Item.InputNotes {
		stored, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusProofReady, stored.Status)
		require.True(t, stored.BroadcastInFlight)
		require.Equal(t, 1, stored.BroadcastAttemptCount)
	}

	_, err = worker.SubmitProofResult(ctx, result)
	require.ErrorContains(t, err, "prior broadcast attempt")
	require.Equal(t, 1, broadcaster.calls)
}

func TestDiscardPublishedProofResultAndReplanIsCoordinated(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	result := chunk.Results[0]
	outbox := NewMemoryProofResultStore()
	require.NoError(t, outbox.SaveProofResult(ctx, result))
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(result.Item.InputNotes))
	for _, note := range result.Item.InputNotes {
		refs = append(refs, privacyreservation.SubmittedReservationRef{
			ReservationID: note.ReservationID,
			LeaseOwner:    result.ReservationLeaseOwners[note.ReservationID],
			LeaseToken:    result.ReservationLeases[note.ReservationID],
		})
	}
	worker := ProofWorker{Reservation: reservationService, ProofResultSink: outbox}
	updated, err := worker.DiscardPublishedProofResultAndReplan(ctx, result.Item.OperationID, refs)
	require.NoError(t, err)
	require.Len(t, updated, len(refs))
	_, err = outbox.GetProofResult(ctx, result.Item.OperationID)
	require.ErrorContains(t, err, "discarded")
	for _, reservation := range updated {
		require.Equal(t, privacyreservation.StatusReplanRequired, reservation.Status)
	}
}

func TestDiscardPublishedProofResultFailureLeavesRetryBlockingMarker(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	result := chunk.Results[0]
	outbox := &discardFailingProofResultStore{MemoryProofResultStore: NewMemoryProofResultStore()}
	require.NoError(t, outbox.SaveProofResult(ctx, result))
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(result.Item.InputNotes))
	for _, note := range result.Item.InputNotes {
		refs = append(refs, privacyreservation.SubmittedReservationRef{
			ReservationID: note.ReservationID,
			LeaseOwner:    result.ReservationLeaseOwners[note.ReservationID],
			LeaseToken:    result.ReservationLeases[note.ReservationID],
		})
	}

	worker := ProofWorker{Reservation: reservationService, ProofResultSink: outbox}
	_, err := worker.DiscardPublishedProofResultAndReplan(ctx, result.Item.OperationID, refs)
	require.ErrorContains(t, err, "proof outbox delete failed")
	for _, ref := range refs {
		stored, getErr := store.GetReservation(ctx, ref.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusProofReady, stored.Status)
		require.True(t, stored.ProofDiscardInFlight)
	}
	_, _, err = reservationService.MarkBroadcastAttempting(ctx, refs, []string{result.Item.OperationID}, privacyreservation.BroadcastAttemptStart{})
	require.Error(t, err)
}

func TestDiscardPublishedProofResultTombstonesStagedRecovery(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryProofResultStore()
	result := ProofResult{Item: PayrollPlanItem{OperationID: "op-discard"}}
	require.NoError(t, store.StageProofResult(ctx, result))
	require.NoError(t, store.DiscardPublishedProofResult(ctx, "op-discard"))

	_, err := store.GetStagedProofResult(ctx, "op-discard")
	require.ErrorContains(t, err, "was discarded")
	require.ErrorContains(t, store.PublishStagedProofResult(ctx, "op-discard"), "was discarded")
	require.ErrorContains(t, store.StageProofResult(ctx, result), "was discarded")
}

func TestBroadcastWorkerFailsClosedOnBroadcastErrorWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	planner := Service{Reservation: reservationService, Now: func() time.Time { return now }}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	result, err := proofWorker.Process(ctx, item)
	require.NoError(t, err)

	broadcastWorker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: noMetadataErrorBroadcaster{err: errors.New("rpc connection reset")},
		Assembler:   fakeAssembler{},
		LeaseOwner:  "broadcast-worker-a",
		LeaseTTL:    time.Minute,
	}
	_, err = broadcastWorker.SubmitProofResult(ctx, *result)
	require.ErrorContains(t, err, "rpc connection reset")

	operation, err := store.GetOperation(ctx, item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusManualReview, operation.Status)
	require.Empty(t, operation.TxHash)
	require.Empty(t, operation.TxBytesHash)
	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusManualReview, reservation.Status)
		require.Empty(t, reservation.LeaseOwner)
		require.Empty(t, reservation.LeaseToken)
		require.Empty(t, reservation.TxHash)
		require.Empty(t, reservation.TxBytesHash)
		require.Equal(t, "rpc connection reset", reservation.LastBroadcastError)
		require.Equal(t, 1, reservation.BroadcastAttemptCount)
	}
}

func TestBroadcastWorkerTreatsNilPreparedResultAsAmbiguous(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	result := chunk.Results[0]
	worker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: nilPreparedResultBroadcaster{},
		Assembler:   fakeAssembler{},
	}

	broadcast, err := worker.SubmitProofResult(ctx, result)
	require.Nil(t, broadcast)
	require.ErrorContains(t, err, "message broadcaster returned nil result")
	operation, getErr := store.GetOperation(ctx, result.Item.OperationID)
	require.NoError(t, getErr)
	require.Equal(t, privacyreservation.OperationStatusManualReview, operation.Status)
	for _, note := range result.Item.InputNotes {
		reservation, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusManualReview, reservation.Status)
	}
}

func TestProofWorkerRollsBackProvingReservationsOnPayloadFailure(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  failingPayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	_, err = proofWorker.Process(ctx, item)
	require.ErrorContains(t, err, "payload failed")

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
		require.Empty(t, reservation.LeaseToken)
	}
}

func TestProofWorkerDoesNotRollbackExpiredProvingLeaseWithoutToken(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	planner := Service{Reservation: reservationService, Now: func() time.Time { return now }}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation: reservationService,
		PayloadBuilder: expiringPayloadBuilder{advance: func() {
			now = now.Add(2 * time.Minute)
		}},
		ProofRunner:     fakeProofRunner{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	_, err = proofWorker.Process(ctx, item)
	require.ErrorContains(t, err, "payload failed after lease expiry")
	require.ErrorIs(t, err, privacyreservation.ErrLeaseUnavailable)

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProving, reservation.Status)
		require.Equal(t, "proof-worker-a", reservation.LeaseOwner)
	}
}

func TestProofWorkerHeartbeatsDuringLongProof(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	now := func() time.Time { return time.Now().UTC() }
	reservationService := privacyreservation.Service{Store: store, Now: now}
	planner := Service{Reservation: reservationService, Now: now}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     slowProofRunner{},
		ProofResultSink: NewMemoryProofResultStore(),
		Assembler:       fakeAssembler{},
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        30 * time.Millisecond,
	}
	_, err = proofWorker.Process(ctx, item)
	require.NoError(t, err)

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		require.True(t, reservation.LeaseUntil.After(time.Now().UTC()), "expected heartbeat to extend lease")
	}
}

func TestProofWorkerKeepsHeartbeatUntilProofReadyIsRecorded(t *testing.T) {
	ctx := context.Background()
	store := &delayedProofReadyReservationStore{
		MemoryStore: privacyreservation.NewMemoryStore(),
		delay:       300 * time.Millisecond,
	}
	now := func() time.Time { return time.Now().UTC() }
	reservationService := privacyreservation.Service{Store: store, Now: now}
	planner := Service{Reservation: reservationService, Now: now}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		ProofResultSink: NewMemoryProofResultStore(),
		Assembler:       fakeAssembler{},
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        100 * time.Millisecond,
	}
	_, err = worker.Process(ctx, confirmed.Items[0])
	require.NoError(t, err)

	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
	}
}

func TestProofWorkerCanonicalizesLeaseOwnerBeforeLeaseLifecycle(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      " proof-worker-a ",
		LeaseTTL:        time.Minute,
	}
	result, err := worker.Process(ctx, item)
	require.NoError(t, err)

	for _, note := range item.InputNotes {
		require.Equal(t, "proof-worker-a", result.ReservationLeaseOwners[note.ReservationID])
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		require.Equal(t, "proof-worker-a", reservation.LeaseOwner)
	}
}

func TestProofWorkerDoesNotPersistProofResultAfterHeartbeatFailure(t *testing.T) {
	ctx := context.Background()
	store := &heartbeatFailingReservationStore{
		MemoryStore:   privacyreservation.NewMemoryStore(),
		heartbeatSeen: make(chan struct{}),
	}
	now := func() time.Time { return time.Now().UTC() }
	reservationService := privacyreservation.Service{Store: store, Now: now}
	planner := Service{Reservation: reservationService, Now: now}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	proofStore := NewMemoryProofResultStore()

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     heartbeatAwareProofRunner{heartbeatSeen: store.heartbeatSeen},
		ProofResultSink: proofStore,
		Assembler:       fakeAssembler{},
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        200 * time.Millisecond,
	}
	_, err = proofWorker.Process(ctx, item)
	require.ErrorIs(t, err, context.Canceled)

	_, err = proofStore.GetProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "not found")
	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
		require.Empty(t, reservation.LeaseToken)
	}
}

func TestProofWorkerDiscardsStagedProofWhenProofReadyTransitionFails(t *testing.T) {
	ctx := context.Background()
	store := &rejectingProofReadyReservationStore{MemoryStore: privacyreservation.NewMemoryStore()}
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(ctx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	proofStore := NewMemoryProofResultStore()

	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: proofStore,
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	_, err = worker.Process(ctx, item)
	require.ErrorContains(t, err, "proof ready rejected")
	_, err = proofStore.GetProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "not found")
	_, err = proofStore.GetStagedProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "not found")
	for _, note := range item.InputNotes {
		reservation, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
	}
}

func TestProofWorkerRejectsInvalidArtifactBeforeStaging(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(ctx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	proofStore := NewMemoryProofResultStore()
	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     invalidHexProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: proofStore,
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}

	_, err = worker.Process(ctx, item)
	require.ErrorContains(t, err, "invalid proof hex")
	_, err = proofStore.GetStagedProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "not found")
	for _, note := range item.InputNotes {
		stored, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusReserved, stored.Status)
	}
}

func TestProofWorkerBoundsAmbiguousStageConfirmationByCallerDeadline(t *testing.T) {
	setupCtx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(setupCtx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(setupCtx, *plan)
	require.NoError(t, err)
	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: &blockingStageConfirmationStore{MemoryProofResultStore: NewMemoryProofResultStore()},
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	ctx, cancel := context.WithTimeout(setupCtx, 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err = worker.Process(ctx, confirmed.Items[0])
	require.ErrorContains(t, err, "ambiguous stage write")
	require.ErrorContains(t, err, "confirm staged proof result")
	require.Less(t, time.Since(started), 2*time.Second)
}

func TestProofWorkerBoundsProofReadyConfirmationByCallerDeadline(t *testing.T) {
	setupCtx := context.Background()
	store := &blockingProofReadyConfirmationStore{MemoryStore: privacyreservation.NewMemoryStore()}
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(setupCtx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(setupCtx, *plan)
	require.NoError(t, err)
	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	ctx, cancel := context.WithTimeout(setupCtx, 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err = worker.Process(ctx, confirmed.Items[0])
	require.ErrorContains(t, err, "proof-ready response lost")
	require.ErrorContains(t, err, "confirm proof-ready reservation batch")
	require.Less(t, time.Since(started), 2*time.Second)
}

func TestProofWorkerCleansUpStageResponseLoss(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(ctx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	proofStore := &stageResponseLostProofResultStore{MemoryProofResultStore: NewMemoryProofResultStore()}
	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: proofStore,
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}

	_, err = worker.Process(ctx, item)
	require.ErrorContains(t, err, "stage response lost")
	_, err = proofStore.GetStagedProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "not found")
	for _, note := range item.InputNotes {
		stored, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusReserved, stored.Status)
	}
}

func TestProofWorkerRecoversProofReadyCASResponseLoss(t *testing.T) {
	ctx := context.Background()
	store := &proofReadyResponseLostStore{MemoryStore: privacyreservation.NewMemoryStore()}
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(ctx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	proofStore := NewMemoryProofResultStore()
	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: proofStore,
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}

	result, err := worker.Process(ctx, item)
	require.NoError(t, err)
	require.NotNil(t, result)
	_, err = proofStore.GetProofResult(ctx, item.OperationID)
	require.NoError(t, err)
	for _, note := range item.InputNotes {
		stored, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusProofReady, stored.Status)
	}
}

func TestProofWorkerCleansUpStagedProofAfterParentContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &contextAwareProofCleanupReservationStore{MemoryStore: privacyreservation.NewMemoryStore()}
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(ctx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	proofStore := &cancelingStageProofResultStore{
		MemoryProofResultStore: NewMemoryProofResultStore(),
		cancel:                 cancel,
	}

	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: proofStore,
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	_, err = worker.Process(ctx, item)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, proofStore.discardedWithLiveContext)
	require.True(t, store.rollbackWithLiveContext)
	_, err = proofStore.GetStagedProofResult(context.Background(), item.OperationID)
	require.ErrorContains(t, err, "not found")
	for _, note := range item.InputNotes {
		reservation, getErr := store.GetReservation(context.Background(), note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
	}
}

func TestProofWorkerRecoversStagedProofAfterPublishInterruption(t *testing.T) {
	ctx := context.Background()
	store := &recordingReservationFilterStore{MemoryStore: privacyreservation.NewMemoryStore()}
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(ctx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	proofStore := &publishFailingProofResultStore{
		MemoryProofResultStore: NewMemoryProofResultStore(),
		failPublish:            true,
	}

	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: proofStore,
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	_, err = worker.Process(ctx, item)
	require.ErrorContains(t, err, "publish staged proof result")
	_, err = proofStore.GetProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "not found")
	_, err = proofStore.GetStagedProofResult(ctx, item.OperationID)
	require.NoError(t, err)
	for _, note := range item.InputNotes {
		reservation, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
	}
	staged := proofStore.staged[item.OperationID]
	allInputNotes := append([]TreasuryNote(nil), staged.Item.InputNotes...)
	staged.Item.InputNotes = staged.Item.InputNotes[:1]
	proofStore.staged[item.OperationID] = staged
	store.filters = nil
	_, err = worker.RecoverStagedProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "missing linked reservation")
	require.NotEmpty(t, store.filters)
	for _, filter := range store.filters {
		require.Equal(t, item.OperationID, filter.OperationID)
	}
	staged.Item.InputNotes = allInputNotes
	proofStore.staged[item.OperationID] = staged
	tampered := staged
	tampered.Payload.Outputs = append([]privacytransfer.PreparedTransferOutput(nil), staged.Payload.Outputs...)
	tampered.Payload.Outputs[0].CommitmentHex = "tampered-commitment"
	proofStore.staged[item.OperationID] = tampered
	_, err = worker.RecoverStagedProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "payload hash mismatch")
	proofStore.staged[item.OperationID] = staged
	tampered = staged
	tampered.Message = &privacytypes.MsgTransfer{Nullifiers: [][]byte{[]byte("different-nullifier")}}
	proofStore.staged[item.OperationID] = tampered
	_, err = worker.RecoverStagedProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "message does not match payload and proof")
	proofStore.staged[item.OperationID] = staged

	recovered, err := worker.RecoverStagedProofResult(ctx, item.OperationID)
	require.NoError(t, err)
	require.NotNil(t, recovered.Message)
	_, err = proofStore.GetProofResult(ctx, item.OperationID)
	require.NoError(t, err)
}

func TestProofWorkerRecoversPublishedProofAfterPublishResponseLoss(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(ctx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	proofStore := &publishFailingProofResultStore{
		MemoryProofResultStore: NewMemoryProofResultStore(),
		failAfterPublish:       true,
	}
	worker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: proofStore,
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}

	_, err = worker.Process(ctx, item)
	require.ErrorContains(t, err, "publisher response lost")
	_, err = proofStore.GetStagedProofResult(ctx, item.OperationID)
	require.Error(t, err)
	_, err = proofStore.GetProofResult(ctx, item.OperationID)
	require.NoError(t, err)

	recovered, err := worker.RecoverStagedProofResult(ctx, item.OperationID)
	require.NoError(t, err)
	require.Equal(t, item.OperationID, recovered.Item.OperationID)
}

func TestProofWorkerBlocksSpentInputBeforeCallingProver(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}
	plan, err := planner.CreatePlan(ctx, testPayrollInput(), []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	runner := &recordingPreparedProofRunner{}
	worker := ProofWorker{
		Reservation:      reservationService,
		PayloadBuilder:   fakePayloadBuilder{},
		ProofRunner:      runner,
		Assembler:        fakeAssembler{},
		ProofResultSink:  NewMemoryProofResultStore(),
		NullifierChecker: fixedProofNullifierChecker{used: map[string]bool{strings.ToLower(item.OperationID) + "-a": true, strings.ToLower(item.OperationID) + "-b": false}},
		LeaseOwner:       "proof-worker-a",
		LeaseTTL:         time.Minute,
	}
	_, err = worker.Process(ctx, item)
	require.ErrorContains(t, err, "already spent")
	require.False(t, runner.called)
	for _, note := range item.InputNotes {
		reservation, getErr := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
	}
}

func TestProofWorkerDoesNotTakeOverProofReadyLease(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	planner := Service{Reservation: reservationService, Now: func() time.Time { return now }}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	result, err := proofWorker.Process(ctx, item)
	require.NoError(t, err)
	firstToken := result.ReservationLeases[item.InputNotes[0].ReservationID]

	now = now.Add(2 * time.Minute)
	_, err = proofWorker.Process(ctx, item)
	require.ErrorIs(t, err, privacyreservation.ErrCompareAndSetFailed)

	reservation, err := store.GetReservation(ctx, item.InputNotes[0].ReservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
	require.Equal(t, firstToken, reservation.LeaseToken)
}

func TestClassifyBroadcastError(t *testing.T) {
	require.Equal(t, RetryActionReconcileUnknown, ClassifyBroadcastError("rpc timeout").Action)
	require.Equal(t, RetryActionRebuildTx, ClassifyBroadcastError("account sequence mismatch").Action)
	require.Equal(t, RetryActionReplan, ClassifyBroadcastError("invalid proof").Action)
	require.Equal(t, RetryActionMarkConflictSpent, ClassifyBroadcastError("nullifier already spent").Action)
	require.Equal(t, RetryActionManualReview, ClassifyBroadcastError("something else").Action)
	require.Equal(t, RetryActionManualReview, ClassifyBroadcastError(&ManualReviewBroadcastError{Cause: errors.New("rpc timeout")}).Action)
}

type fakePayloadBuilder struct{}

func (fakePayloadBuilder) BuildPreparedTransferPayload(_ context.Context, item PayrollPlanItem) (*privacytransfer.PreparedTransferPayload, error) {
	prefix := item.OperationID
	if prefix == "" {
		prefix = "operation"
	}
	payload := privacytransfer.PreparedTransferPayload{
		Version:                  privacytransfer.PreparedTransferPayloadVersion,
		AuditDisclosureDigestHex: "audit-digest-a",
		Inputs: []privacytransfer.PreparedTransferInput{
			{NullifierHex: prefix + "-a"},
			{NullifierHex: prefix + "-b"},
		},
		Outputs: []privacytransfer.PreparedTransferOutput{
			{CommitmentHex: "commitment-a"},
			{CommitmentHex: "change-a"},
		},
	}
	payload.PayloadHash = privacytransfer.ComputePreparedTransferPayloadHash(payload)
	return &payload, nil
}

func (fakePayloadBuilder) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type multiPlanePayloadBuilder struct {
	payload privacytransfer.PreparedTransferPayload
}

func (b multiPlanePayloadBuilder) BuildPreparedTransferPayload(_ context.Context, _ PayrollPlanItem) (*privacytransfer.PreparedTransferPayload, error) {
	payload := b.payload
	payload.Inputs = append([]privacytransfer.PreparedTransferInput(nil), b.payload.Inputs...)
	payload.Outputs = append([]privacytransfer.PreparedTransferOutput(nil), b.payload.Outputs...)
	payload.PayloadHash = privacytransfer.ComputePreparedTransferPayloadHash(payload)
	return &payload, nil
}

func (multiPlanePayloadBuilder) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}

type failingPayloadBuilder struct{}

func (failingPayloadBuilder) BuildPreparedTransferPayload(_ context.Context, _ PayrollPlanItem) (*privacytransfer.PreparedTransferPayload, error) {
	return nil, fmt.Errorf("payload failed")
}

type expiringPayloadBuilder struct {
	advance func()
}

func (b expiringPayloadBuilder) BuildPreparedTransferPayload(_ context.Context, _ PayrollPlanItem) (*privacytransfer.PreparedTransferPayload, error) {
	if b.advance != nil {
		b.advance()
	}
	return nil, fmt.Errorf("payload failed after lease expiry")
}

type fakeProofRunner struct{}

func (fakeProofRunner) BuildPreparedTransferProof(_ context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "aa",
	}, nil
}

type invalidHexProofRunner struct{}

func (invalidHexProofRunner) BuildPreparedTransferProof(_ context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "not-hex",
	}, nil
}

type slowProofRunner struct{}

func (r slowProofRunner) BuildPreparedTransferProof(ctx context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	for i := 0; i < 6; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "aa",
	}, nil
}

type heartbeatAwareProofRunner struct {
	heartbeatSeen <-chan struct{}
}

func (r heartbeatAwareProofRunner) BuildPreparedTransferProof(_ context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	select {
	case <-r.heartbeatSeen:
	case <-time.After(time.Second):
		return nil, fmt.Errorf("heartbeat was not observed")
	}
	time.Sleep(20 * time.Millisecond)
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "aa",
	}, nil
}

type heartbeatFailingReservationStore struct {
	*privacyreservation.MemoryStore
	heartbeatSeen chan struct{}
	closeOnce     sync.Once
}

type delayedProofReadyReservationStore struct {
	*privacyreservation.MemoryStore
	delay time.Duration
}

type rejectingProofReadyReservationStore struct {
	*privacyreservation.MemoryStore
}

type proofReadyResponseLostStore struct {
	*privacyreservation.MemoryStore
	failOnce bool
}

type terminalWriteFailingReservationStore struct {
	*privacyreservation.MemoryStore
}

type allTerminalWritesFailingReservationStore struct {
	*privacyreservation.MemoryStore
}

type contextAwareProofCleanupReservationStore struct {
	*privacyreservation.MemoryStore
	rollbackWithLiveContext bool
}

func (s *contextAwareProofCleanupReservationStore) MarkReservationsProofReady(ctx context.Context, refs []privacyreservation.SubmittedReservationRef, update privacyreservation.ProofReadyOperationUpdate, now time.Time) ([]privacyreservation.NoteReservation, *privacyreservation.PayrollOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return s.MemoryStore.MarkReservationsProofReady(ctx, refs, update, now)
}

func (s *contextAwareProofCleanupReservationStore) RollbackProvingOperation(ctx context.Context, operationID string, refs []privacyreservation.SubmittedReservationRef, now time.Time) ([]privacyreservation.NoteReservation, *privacyreservation.PayrollOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.rollbackWithLiveContext = true
	return s.MemoryStore.RollbackProvingOperation(ctx, operationID, refs, now)
}

func (s *rejectingProofReadyReservationStore) MarkReservationsProofReady(_ context.Context, _ []privacyreservation.SubmittedReservationRef, _ privacyreservation.ProofReadyOperationUpdate, _ time.Time) ([]privacyreservation.NoteReservation, *privacyreservation.PayrollOperation, error) {
	return nil, nil, errors.New("proof ready rejected")
}

type publishFailingProofResultStore struct {
	*MemoryProofResultStore
	failPublish      bool
	failAfterPublish bool
}

type cancelingStageProofResultStore struct {
	*MemoryProofResultStore
	cancel                   context.CancelFunc
	discardedWithLiveContext bool
}

type stageResponseLostProofResultStore struct {
	*MemoryProofResultStore
}

type blockingStageConfirmationStore struct {
	*MemoryProofResultStore
}

func (s *blockingStageConfirmationStore) StageProofResult(context.Context, ProofResult) error {
	return errors.New("ambiguous stage write")
}

func (s *blockingStageConfirmationStore) GetStagedProofResult(ctx context.Context, _ string) (*ProofResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type blockingProofReadyConfirmationStore struct {
	*privacyreservation.MemoryStore
	confirming bool
}

func (s *blockingProofReadyConfirmationStore) MarkReservationsProofReady(context.Context, []privacyreservation.SubmittedReservationRef, privacyreservation.ProofReadyOperationUpdate, time.Time) ([]privacyreservation.NoteReservation, *privacyreservation.PayrollOperation, error) {
	s.confirming = true
	return nil, nil, errors.New("proof-ready response lost")
}

func (s *blockingProofReadyConfirmationStore) GetOperation(ctx context.Context, operationID string) (*privacyreservation.PayrollOperation, error) {
	if s.confirming {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.MemoryStore.GetOperation(ctx, operationID)
}

type recordingReservationFilterStore struct {
	*privacyreservation.MemoryStore
	filters []privacyreservation.ReservationFilter
}

func (s *recordingReservationFilterStore) ListReservations(ctx context.Context, filter privacyreservation.ReservationFilter) ([]privacyreservation.NoteReservation, error) {
	s.filters = append(s.filters, filter)
	return s.MemoryStore.ListReservations(ctx, filter)
}

func (s *stageResponseLostProofResultStore) StageProofResult(ctx context.Context, result ProofResult) error {
	if err := s.MemoryProofResultStore.StageProofResult(ctx, result); err != nil {
		return err
	}
	return errors.New("stage response lost")
}

func (s *proofReadyResponseLostStore) MarkReservationsProofReady(ctx context.Context, refs []privacyreservation.SubmittedReservationRef, update privacyreservation.ProofReadyOperationUpdate, now time.Time) ([]privacyreservation.NoteReservation, *privacyreservation.PayrollOperation, error) {
	updated, operation, err := s.MemoryStore.MarkReservationsProofReady(ctx, refs, update, now)
	if err != nil {
		return nil, nil, err
	}
	if !s.failOnce {
		s.failOnce = true
		return updated, operation, errors.New("proof-ready response lost")
	}
	return updated, operation, nil
}

func (s *terminalWriteFailingReservationStore) MarkReservationsSubmitted(context.Context, []privacyreservation.SubmittedReservationRef, []string, privacyreservation.SubmittedReservationUpdate, time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	return nil, nil, errors.New("submitted write failed")
}

func (s *terminalWriteFailingReservationStore) MarkReservationsBroadcastUnknown(context.Context, []privacyreservation.SubmittedReservationRef, []string, privacyreservation.BroadcastAttemptUpdate, time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	return nil, nil, errors.New("unknown write failed")
}

func (s *allTerminalWritesFailingReservationStore) MarkReservationsSubmitted(context.Context, []privacyreservation.SubmittedReservationRef, []string, privacyreservation.SubmittedReservationUpdate, time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	return nil, nil, errors.New("submitted write failed")
}

func (s *allTerminalWritesFailingReservationStore) MarkReservationsBroadcastUnknown(context.Context, []privacyreservation.SubmittedReservationRef, []string, privacyreservation.BroadcastAttemptUpdate, time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	return nil, nil, errors.New("unknown write failed")
}

func (s *allTerminalWritesFailingReservationStore) MarkReservationsBroadcastAmbiguous(context.Context, []privacyreservation.SubmittedReservationRef, []string, privacyreservation.BroadcastAmbiguityUpdate, time.Time) ([]privacyreservation.NoteReservation, []privacyreservation.PayrollOperation, error) {
	return nil, nil, errors.New("ambiguous write failed")
}

func (s *cancelingStageProofResultStore) StageProofResult(ctx context.Context, result ProofResult) error {
	if err := s.MemoryProofResultStore.StageProofResult(ctx, result); err != nil {
		return err
	}
	s.cancel()
	return nil
}

func (s *cancelingStageProofResultStore) DiscardStagedProofResult(ctx context.Context, operationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.discardedWithLiveContext = true
	return s.MemoryProofResultStore.DiscardStagedProofResult(ctx, operationID)
}

func (s *publishFailingProofResultStore) PublishStagedProofResult(ctx context.Context, operationID string) error {
	if s.failPublish {
		s.failPublish = false
		return errors.New("proof result publisher interrupted")
	}
	if s.failAfterPublish {
		s.failAfterPublish = false
		if err := s.MemoryProofResultStore.PublishStagedProofResult(ctx, operationID); err != nil {
			return err
		}
		return errors.New("proof result publisher response lost")
	}
	return s.MemoryProofResultStore.PublishStagedProofResult(ctx, operationID)
}

type fixedProofNullifierChecker struct {
	used map[string]bool
}

func (c fixedProofNullifierChecker) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	result := make(map[string]bool, len(nullifierHexes))
	for _, nullifier := range nullifierHexes {
		result[nullifier] = c.used[nullifier]
	}
	return result, nil
}

type recordingPreparedProofRunner struct {
	called bool
}

func (r *recordingPreparedProofRunner) BuildPreparedTransferProof(_ context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	r.called = true
	return &privacytransfer.PreparedTransferProof{Version: privacytransfer.PreparedTransferProofVersion, PayloadHash: payload.PayloadHash, ProofHex: "aa"}, nil
}

func (s *delayedProofReadyReservationStore) MarkReservationsProofReady(ctx context.Context, refs []privacyreservation.SubmittedReservationRef, update privacyreservation.ProofReadyOperationUpdate, _ time.Time) ([]privacyreservation.NoteReservation, *privacyreservation.PayrollOperation, error) {
	time.Sleep(s.delay)
	return s.MemoryStore.MarkReservationsProofReady(ctx, refs, update, time.Now().UTC())
}

func (s *heartbeatFailingReservationStore) HeartbeatReservationLease(_ context.Context, _ string, _ string, _ string, _ time.Time, _ time.Time) (*privacyreservation.NoteReservation, error) {
	s.closeOnce.Do(func() {
		close(s.heartbeatSeen)
	})
	return nil, privacyreservation.ErrLeaseUnavailable
}

type fakeAssembler struct{}

type discardFailingProofResultStore struct {
	*MemoryProofResultStore
}

func (s *discardFailingProofResultStore) DiscardPublishedProofResult(context.Context, string) error {
	return fmt.Errorf("proof outbox delete failed")
}

func (fakeAssembler) BuildTransferMessage(payload privacytransfer.PreparedTransferPayload, _ privacytransfer.PreparedTransferProof) (*privacytypes.MsgTransfer, error) {
	message := &privacytypes.MsgTransfer{Nullifiers: make([][]byte, 0, len(payload.Inputs))}
	for _, input := range payload.Inputs {
		message.Nullifiers = append(message.Nullifiers, []byte(input.NullifierHex))
	}
	return message, nil
}

type fakeBroadcaster struct{}

func (fakeBroadcaster) BroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*BroadcastResult, error) {
	if len(msgs) != 1 {
		return nil, nil
	}
	return &BroadcastResult{
		TxHash:          "TXHASH",
		TxBytesHash:     "tx-bytes-hash",
		SignDocHash:     "sign-doc-hash",
		AccountSequence: 7,
		Height:          11,
	}, nil
}

func (fakeBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}
