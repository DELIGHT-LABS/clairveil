package reservation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceReserveRejectsActiveDuplicate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "")})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r2", "note-b", "")})
	if !errors.Is(err, ErrActiveReservationExists) {
		t.Fatalf("expected active duplicate rejection, got %v", err)
	}
}

func TestServiceReserveLinksOperationToReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	reservation := testReservation("r1", "note-a", "")
	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: reservation,
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.OperationID != "op-a" {
		t.Fatalf("expected reservation operation_id to be populated, got %+v", created)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.ReservationID != "r1" {
		t.Fatalf("expected operation reservation_id to be populated, got %+v", operation)
	}
}

func TestServiceReserveRejectsMissingOperationLink(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "op-a")})
	if !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("expected missing operation link rejection, got %v", err)
	}
}

func TestServiceReserveRejectsOperationLinkMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-b", Status: OperationStatusPlanned},
	})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected operation_id mismatch rejection, got %v", err)
	}

	_, err = svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:   "op-a",
			ReservationID: "different-reservation",
			Status:        OperationStatusPlanned,
		},
	})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected reservation_id mismatch rejection, got %v", err)
	}
}

func TestServiceTransitionUsesCompareAndSet(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := svc.Transition(ctx, created.ReservationID, StatusReserved, StatusReleased)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusReleased {
		t.Fatalf("expected Released got %s", updated.Status)
	}

	_, err = svc.Transition(ctx, created.ReservationID, StatusReserved, StatusReplanRequired)
	if !errors.Is(err, ErrCompareAndSetFailed) {
		t.Fatalf("expected compare-and-set failure, got %v", err)
	}
}

func TestServiceTransitionRejectsActiveDuplicate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "")})
	if err != nil {
		t.Fatal(err)
	}
	replanning := testReservation("r2", "note-b", "op-b")
	replanning.Status = StatusReplanRequired
	if _, err := store.CreateReservation(ctx, replanning); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Transition(ctx, "r2", StatusReplanRequired, StatusReserved)
	if !errors.Is(err, ErrActiveReservationExists) {
		t.Fatalf("expected active duplicate rejection, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusReplanRequired {
		t.Fatalf("expected duplicate transition to leave status ReplanRequired, got %s", unchanged.Status)
	}
}

func TestServiceTransitionRejectsLeaseRequiredTransitionWithoutToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Transition(ctx, created.ReservationID, StatusReserved, StatusProving)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected lease mismatch for worker-owned transition, got %v", err)
	}
}

func TestMemoryStoreCompareAndSetRejectsLeaseRequiredTransitionWithoutToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.CompareAndSetReservationStatus(ctx, created.ReservationID, StatusReserved, StatusProving, fixedNow())
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected store CAS to require lease token, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusReserved {
		t.Fatalf("expected reservation to remain Reserved, got %s", unchanged.Status)
	}
}

func TestMemoryStoreCompareAndSetWithOperationRejectsLeaseRequiredTransitionWithoutToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, _ := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	_, _, err := store.CompareAndSetReservationStatusWithOperation(ctx, created.ReservationID, StatusProofReady, StatusSubmitted, &PayrollOperation{
		OperationID: "op-a",
		Status:      OperationStatusSubmitted,
		TxHash:      "tx",
	}, fixedNow())
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected store CAS with operation to require lease token, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusProofReady {
		t.Fatalf("expected reservation to remain ProofReady, got %s", unchanged.Status)
	}
}

func TestServiceReleaseOnlyReservedAutomatically(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Release(ctx, created.ReservationID, StatusProving)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid release transition, got %v", err)
	}
	_, err = svc.Transition(ctx, created.ReservationID, StatusProving, StatusReleased)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected direct Proving -> Released transition to be rejected, got %v", err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Transition(ctx, created.ReservationID, StatusProofReady, StatusReleased)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected direct ProofReady -> Released transition to be rejected, got %v", err)
	}
}

func TestServiceMarkSubmittedRejectsStaleLeaseAfterTakeover(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, first.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, first.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Minute)
	second, err := svc.AcquireLease(ctx, created.ReservationID, "worker-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.MarkSubmitted(ctx, created.ReservationID, first.Token, "stale-tx", "stale-bytes", "stale-sign-doc", 1)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected stale submitted token mismatch, got %v", err)
	}

	submitted, err := svc.MarkSubmitted(ctx, created.ReservationID, second.Token, "fresh-tx", "fresh-bytes", "fresh-sign-doc", 2)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Status != StatusSubmitted || submitted.TxHash != "fresh-tx" {
		t.Fatalf("expected fresh submit to win, got %+v", submitted)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusSubmitted || operation.TxHash != "fresh-tx" || operation.TxBytesHash != "fresh-bytes" || operation.SignDocHash != "fresh-sign-doc" {
		t.Fatalf("expected submitted operation metadata, got %+v", operation)
	}
}

func TestMemoryStoreMarkReservationSubmittedDerivesLinkedOperation(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}

	_, err = store.MarkReservationSubmitted(ctx, created.ReservationID, lease.Token, SubmittedReservationUpdate{
		TxHash:          "txhash",
		TxBytesHash:     "tx-bytes",
		SignDocHash:     "sign-doc",
		AccountSequence: 3,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusSubmitted || operation.TxHash != "txhash" || operation.TxBytesHash != "tx-bytes" || operation.SignDocHash != "sign-doc" {
		t.Fatalf("expected legacy submitted path to update linked operation, got %+v", operation)
	}
}

func TestMemoryStoreMarkSubmittedRejectsMissingTxIdentity(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	_, err := store.MarkReservationSubmitted(ctx, created.ReservationID, lease.Token, SubmittedReservationUpdate{}, now)
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected missing tx identity rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProofReady {
		t.Fatalf("expected reservation to remain ProofReady, got %s", reservation.Status)
	}
}

func TestServiceTransitionRejectsBroadcastUnknownWithoutToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Transition(ctx, created.ReservationID, StatusProofReady, StatusUnknown)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected lease mismatch for ProofReady -> Unknown, got %v", err)
	}

	updated, operations, err := svc.MarkBroadcastUnknownBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseToken:    lease.Token,
	}}, nil, BroadcastAttemptUpdate{
		TxHash:             "ambiguous-tx",
		TxBytesHash:        "ambiguous-bytes",
		SignDocHash:        "ambiguous-sign-doc",
		LastBroadcastError: "rpc timeout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].Status != StatusUnknown || updated[0].TxHash != "ambiguous-tx" {
		t.Fatalf("expected leased broadcast-unknown update, got %+v", updated)
	}
	if len(operations) != 1 || operations[0].Status != OperationStatusUnknown || operations[0].TxHash != "ambiguous-tx" {
		t.Fatalf("expected linked operation unknown metadata, got %+v", operations)
	}
}

func TestServiceMarkBroadcastUnknownRejectsMissingTxIdentity(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	_, _, err := svc.MarkBroadcastUnknownBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseToken:    lease.Token,
	}}, nil, BroadcastAttemptUpdate{LastBroadcastError: "rpc timeout"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected missing tx identity rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProofReady {
		t.Fatalf("expected reservation to remain ProofReady, got %s", reservation.Status)
	}
}

func TestServiceNilStoreMethodsReturnErrors(t *testing.T) {
	ctx := context.Background()
	svc := Service{}

	if _, err := svc.MarkSubmitted(ctx, "r1", "lease-token", "tx", "tx-bytes", "sign-doc", 1); err == nil {
		t.Fatalf("expected MarkSubmitted to reject nil store")
	}
	if _, err := svc.HeartbeatLease(ctx, "r1", "lease-token", time.Minute); err == nil {
		t.Fatalf("expected HeartbeatLease to reject nil store")
	}
	if _, err := svc.ClearLease(ctx, "r1", "lease-token"); err == nil {
		t.Fatalf("expected ClearLease to reject nil store")
	}
}

func TestServiceMarkProofReadyBatchIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	firstReservation := testReservation("r1", "note-a", "op-a")
	secondReservation := testReservation("r2", "note-b", "op-a")
	secondReservation.NullifierLookupKey = "lookup-b"

	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: firstReservation,
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{Reservation: secondReservation},
	})
	if err != nil {
		t.Fatal(err)
	}

	firstLease, err := svc.AcquireLeaseForStatus(ctx, "r1", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	secondLease, err := svc.AcquireLeaseForStatus(ctx, "r2", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", firstLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r2", secondLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseToken: firstLease.Token},
		{ReservationID: "r2", LeaseToken: "stale-token"},
	}, ProofReadyOperationUpdate{
		OperationID:              "op-a",
		ExpectedOutputCommitment: "commitment-a",
	})
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected stale token to reject proof-ready batch, got %v", err)
	}
	for _, reservationID := range []string{"r1", "r2"} {
		reservation, err := store.GetReservation(ctx, reservationID)
		if err != nil {
			t.Fatal(err)
		}
		if reservation.Status != StatusProving {
			t.Fatalf("expected %s to remain Proving, got %s", reservationID, reservation.Status)
		}
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusPlanned || operation.ExpectedOutputCommitment != "" {
		t.Fatalf("expected operation to remain planned, got %+v", operation)
	}

	_, operation, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseToken: firstLease.Token},
		{ReservationID: "r2", LeaseToken: secondLease.Token},
	}, ProofReadyOperationUpdate{
		OperationID:              "op-a",
		ExpectedOutputCommitment: "commitment-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusProofReady || operation.ExpectedOutputCommitment != "commitment-a" {
		t.Fatalf("expected proof-ready operation, got %+v", operation)
	}
	for _, reservationID := range []string{"r1", "r2"} {
		reservation, err := store.GetReservation(ctx, reservationID)
		if err != nil {
			t.Fatal(err)
		}
		if reservation.Status != StatusProofReady {
			t.Fatalf("expected %s to become ProofReady, got %s", reservationID, reservation.Status)
		}
	}
}

func TestServiceMarkProofReadyBatchRejectsLinkedOperationWithoutOperationID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLeaseForStatus(ctx, created.ReservationID, "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{
		{ReservationID: created.ReservationID, LeaseToken: lease.Token},
	}, ProofReadyOperationUpdate{ExpectedOutputCommitment: "commitment-a"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected linked proof-ready update without operation_id to be rejected, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusProving {
		t.Fatalf("expected reservation to remain Proving, got %s", unchanged.Status)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusPlanned || operation.ExpectedOutputCommitment != "" {
		t.Fatalf("expected operation to remain planned without evidence, got %+v", operation)
	}
}

func TestServiceMarkProofReadyBatchRejectsExpectedEvidenceMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:              "op-a",
			ExpectedOutputCommitment: "planned-commitment",
			ExpectedDisclosureDigest: "planned-digest",
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLeaseForStatus(ctx, "r1", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseToken: lease.Token},
	}, ProofReadyOperationUpdate{
		OperationID:              "op-a",
		ExpectedOutputCommitment: "actual-commitment",
		ExpectedDisclosureDigest: "planned-digest",
	})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected expected evidence mismatch rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProving {
		t.Fatalf("expected reservation to remain Proving, got %s", reservation.Status)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusPlanned || operation.ExpectedOutputCommitment != "planned-commitment" || operation.ExpectedDisclosureDigest != "planned-digest" {
		t.Fatalf("expected operation to remain planned with original evidence, got %+v", operation)
	}
}

func TestServiceMarkProofReadyBatchRejectsOperationWithoutReservations(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	if _, err := store.CreateOperation(ctx, PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned}); err != nil {
		t.Fatal(err)
	}

	_, _, err := svc.MarkProofReadyBatch(ctx, nil, ProofReadyOperationUpdate{OperationID: "op-a"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected empty reservation refs to be rejected, got %v", err)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusPlanned {
		t.Fatalf("expected operation to remain planned, got %s", operation.Status)
	}
}

func TestServiceMarkProofReadyBatchRejectsCrossOperationReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{
			Reservation: func() NoteReservation {
				reservation := testReservation("r2", "note-b", "op-b")
				reservation.NullifierLookupKey = "lookup-b"
				return reservation
			}(),
			Operation: &PayrollOperation{OperationID: "op-b", Status: OperationStatusPlanned},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstLease, err := svc.AcquireLeaseForStatus(ctx, "r1", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	secondLease, err := svc.AcquireLeaseForStatus(ctx, "r2", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", firstLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r2", secondLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseToken: firstLease.Token},
		{ReservationID: "r2", LeaseToken: secondLease.Token},
	}, ProofReadyOperationUpdate{OperationID: "op-a"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected cross-operation proof-ready batch to be rejected, got %v", err)
	}
}

func TestServiceMarkSubmittedBatchRejectsCrossOperationIDs(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{
			Reservation: func() NoteReservation {
				reservation := testReservation("r2", "note-b", "op-b")
				reservation.NullifierLookupKey = "lookup-b"
				return reservation
			}(),
			Operation: &PayrollOperation{OperationID: "op-b", Status: OperationStatusPlanned},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstLease, err := svc.AcquireLeaseForStatus(ctx, "r1", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	secondLease, err := svc.AcquireLeaseForStatus(ctx, "r2", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", firstLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", firstLease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r2", secondLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r2", secondLease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkSubmittedBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseToken: firstLease.Token},
	}, []string{"op-b"}, SubmittedReservationUpdate{TxHash: "tx"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected cross-operation submitted batch to be rejected, got %v", err)
	}
}

func TestServiceMarkSubmittedBatchRejectsMissingOperationReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{
			Reservation: func() NoteReservation {
				reservation := testReservation("r2", "note-b", "op-a")
				reservation.NullifierLookupKey = "lookup-b"
				return reservation
			}(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstLease, err := svc.AcquireLeaseForStatus(ctx, "r1", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	secondLease, err := svc.AcquireLeaseForStatus(ctx, "r2", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", firstLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", firstLease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r2", secondLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r2", secondLease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkSubmittedBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseToken: firstLease.Token},
	}, []string{"op-a"}, SubmittedReservationUpdate{TxHash: "tx"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected missing sibling reservation to be rejected, got %v", err)
	}
}

func TestServiceMarkBroadcastUnknownBatchRejectsMissingOperationReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{
			Reservation: func() NoteReservation {
				reservation := testReservation("r2", "note-b", "op-a")
				reservation.NullifierLookupKey = "lookup-b"
				return reservation
			}(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstLease, err := svc.AcquireLeaseForStatus(ctx, "r1", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	secondLease, err := svc.AcquireLeaseForStatus(ctx, "r2", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", firstLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", firstLease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r2", secondLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r2", secondLease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkBroadcastUnknownBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseToken: firstLease.Token},
	}, []string{"op-a"}, BroadcastAttemptUpdate{TxHash: "tx", LastBroadcastError: "rpc timeout"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected missing sibling reservation to be rejected, got %v", err)
	}
}

func TestServiceReusableTransitionsClearStaleLease(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "")})
	if err != nil {
		t.Fatal(err)
	}
	firstLease, err := svc.AcquireLeaseForStatus(ctx, created.ReservationID, "worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, created.ReservationID, StatusReserved, StatusReleased); err != nil {
		t.Fatal(err)
	}
	released, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if released.LeaseToken != "" || !released.LeaseUntil.IsZero() {
		t.Fatalf("expected release transition to clear lease, got %+v", released)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, firstLease.Token, StatusReleased, StatusAvailable); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected stale token to be unusable, got %v", err)
	}
	if _, err := svc.Transition(ctx, created.ReservationID, StatusReleased, StatusAvailable); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, created.ReservationID, StatusAvailable, StatusReserved); err != nil {
		t.Fatal(err)
	}
	secondLease, err := svc.AcquireLeaseForStatus(ctx, created.ReservationID, "worker-b", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if secondLease.Token == firstLease.Token {
		t.Fatal("expected a fresh lease token after reuse")
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC)
}

func reserveProofReadyReservation(t *testing.T, ctx context.Context, svc Service, reservationID string, noteID string, operationID string) (*NoteReservation, *Lease) {
	t.Helper()

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation(reservationID, noteID, operationID),
		Operation:   &PayrollOperation{OperationID: operationID, Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}
	return created, lease
}

func testReservation(id string, noteID string, operationID string) NoteReservation {
	return NoteReservation{
		ReservationID:        id,
		NoteID:               noteID,
		OwnerKeyID:           "owner-a",
		NullifierLookupKey:   "lookup-a",
		NullifierLookupKeyID: "lookup-key-v1",
		Status:               StatusReserved,
		OperationID:          operationID,
	}
}
