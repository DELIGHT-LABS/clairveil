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

	updated, err := svc.Transition(ctx, created.ReservationID, StatusReserved, StatusProving)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusProving {
		t.Fatalf("expected Proving got %s", updated.Status)
	}

	_, err = svc.Transition(ctx, created.ReservationID, StatusReserved, StatusReleased)
	if !errors.Is(err, ErrCompareAndSetFailed) {
		t.Fatalf("expected compare-and-set failure, got %v", err)
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
	if _, err := svc.Transition(ctx, created.ReservationID, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Release(ctx, created.ReservationID, StatusProving)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid release transition, got %v", err)
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
