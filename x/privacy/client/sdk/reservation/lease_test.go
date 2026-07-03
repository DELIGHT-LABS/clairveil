package reservation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceLeaseRejectsConcurrentWorkerAndAllowsHeartbeat(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}

	created, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "op-a")})
	if err != nil {
		t.Fatal(err)
	}

	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AcquireLease(ctx, created.ReservationID, "worker-b", time.Minute)
	if !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("expected unavailable lease, got %v", err)
	}

	now = now.Add(30 * time.Second)
	heartbeat, err := svc.HeartbeatLease(ctx, created.ReservationID, lease.Token, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !heartbeat.Until.After(lease.Until) {
		t.Fatalf("expected heartbeat to extend lease")
	}
}

func TestServiceLeaseRejectsStaleToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "op-a")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute); err != nil {
		t.Fatal(err)
	}

	_, err = svc.HeartbeatLease(ctx, created.ReservationID, "stale-token", time.Minute)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected stale token rejection, got %v", err)
	}
}

func TestServiceTransitionWithLeaseRejectsStaleToken(t *testing.T) {
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

	_, err = svc.TransitionWithLease(ctx, created.ReservationID, "stale-token", StatusReserved, StatusProving)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected stale token rejection, got %v", err)
	}
	updated, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusReserved, StatusProving)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusProving {
		t.Fatalf("expected Proving got %s", updated.Status)
	}
}

func TestServiceLeaseTakeoverRejectsStaleHeartbeatAndClear(t *testing.T) {
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

	now = now.Add(2 * time.Minute)
	second, err := svc.AcquireLease(ctx, created.ReservationID, "worker-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token {
		t.Fatal("expected takeover to issue a new lease token")
	}

	_, err = svc.HeartbeatLease(ctx, created.ReservationID, first.Token, time.Minute)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected stale heartbeat token mismatch, got %v", err)
	}
	_, err = svc.ClearLease(ctx, created.ReservationID, first.Token)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected stale clear token mismatch, got %v", err)
	}

	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.LeaseOwner != "worker-b" || reservation.LeaseToken != second.Token {
		t.Fatalf("stale lease operation overwrote takeover lease: %+v", reservation)
	}
}
