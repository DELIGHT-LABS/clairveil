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

	_, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "op-a")})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r2", "note-b", "op-b")})
	if !errors.Is(err, ErrActiveReservationExists) {
		t.Fatalf("expected active duplicate rejection, got %v", err)
	}
}

func TestServiceTransitionUsesCompareAndSet(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "op-a")})
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

func TestServiceTransitionRejectsLeaseRequiredTransitionWithoutToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "op-a")})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Transition(ctx, created.ReservationID, StatusReserved, StatusProving)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected lease mismatch for worker-owned transition, got %v", err)
	}
}

func TestServiceReleaseOnlyReservedAutomatically(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "op-a")})
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
}

func TestServiceMarkSubmittedRejectsStaleLeaseAfterTakeover(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}

	created, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "op-a")})
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

func fixedNow() time.Time {
	return time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC)
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
