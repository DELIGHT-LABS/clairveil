package reservation_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

// externalPersistentStore models a Store implemented by another Go package.
// It can receive Service-authorized atomic reconcile commands without access to
// reservation package-private methods or fields.
type externalPersistentStore struct {
	reservation.Store
	reconcileCalls int
	commandKinds   []reservation.ReconciliationCommandKind
}

type extendingBeforeSingleLeaseStore struct {
	*reservation.MemoryStore
	once      sync.Once
	injectErr error
}

func (s *extendingBeforeSingleLeaseStore) AcquireSingleReservationLease(ctx context.Context, reservationID string, owner string, leaseToken string, requiredStatus reservation.ReservationStatus, leaseUntil time.Time, now time.Time) (*reservation.NoteReservation, error) {
	s.once.Do(func() {
		injector := reservation.Service{Store: s.MemoryStore, Now: func() time.Time { return now }}
		_, s.injectErr = injector.Reserve(ctx, reservation.ReserveInput{
			Reservation: reservation.NoteReservation{
				ReservationID:      "r2",
				NoteID:             "note-b",
				OwnerKeyID:         "owner-b",
				NullifierLookupKey: "lookup-b",
				OperationID:        "op-a",
				Status:             reservation.StatusReserved,
			},
		})
	})
	if s.injectErr != nil {
		return nil, s.injectErr
	}
	return s.MemoryStore.AcquireSingleReservationLease(ctx, reservationID, owner, leaseToken, requiredStatus, leaseUntil, now)
}

func TestExternalStoreSingleLeaseAtomicallyRejectsConcurrentOperationExtension(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC)
	store := &extendingBeforeSingleLeaseStore{MemoryStore: reservation.NewMemoryStore()}
	svc := reservation.Service{Store: store, Now: func() time.Time { return now }}
	_, err := svc.Reserve(ctx, reservation.ReserveInput{
		Reservation: reservation.NoteReservation{
			ReservationID:      "r1",
			NoteID:             "note-a",
			OwnerKeyID:         "owner-a",
			NullifierLookupKey: "lookup-a",
			OperationID:        "op-a",
			Status:             reservation.StatusReserved,
		},
		Operation: &reservation.PayrollOperation{OperationID: "op-a", Status: reservation.OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.AcquireLease(ctx, "r1", "worker-a", time.Minute)
	if !errors.Is(err, reservation.ErrLeaseUnavailable) {
		t.Fatalf("expected atomic multi-input lease rejection, got %v", err)
	}
	second, err := store.GetReservation(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if second.OperationID != "op-a" || second.LeaseToken != "" {
		t.Fatalf("unexpected concurrently added reservation: %+v", second)
	}
}

func (s *externalPersistentStore) ApplyReconciliationTransition(ctx context.Context, transition reservation.ReconciliationTransition) (*reservation.NoteReservation, *reservation.PayrollOperation, error) {
	s.reconcileCalls++
	s.commandKinds = append(s.commandKinds, transition.CommandKind())
	if transition.QuarantinesMatchingSpent() {
		if transition.ReservationID == "" {
			return nil, nil, fmt.Errorf("spent quarantine must identify its reservation")
		}
	}
	return s.Store.ApplyReconciliationTransition(ctx, transition)
}

func TestExternalStoreCanReceiveServiceReconciliationCommand(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC)
	store := &externalPersistentStore{Store: reservation.NewMemoryStore()}
	svc := reservation.Service{Store: store, Now: func() time.Time { return now }}

	created, err := svc.Reserve(ctx, reservation.ReserveInput{
		Reservation: reservation.NoteReservation{
			ReservationID:      "r1",
			NoteID:             "note-a",
			OwnerKeyID:         "owner-a",
			NullifierLookupKey: "lookup-a",
			OperationID:        "op-a",
			Status:             reservation.StatusReserved,
		},
		Operation: &reservation.PayrollOperation{OperationID: "op-a", Status: reservation.OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, reservation.StatusReserved, reservation.StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkProofReadyBatch(ctx, []reservation.SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, reservation.ProofReadyOperationUpdate{
		OperationID: "op-a",
		PayloadHash: "payload-op-a",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkBroadcastAttempting(ctx, []reservation.SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, []string{"op-a"}, reservation.BroadcastAttemptStart{TxHash: "txhash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkSubmitted(ctx, created.ReservationID, lease.Owner, lease.Token, "txhash", "", "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reconcile(ctx, created.ReservationID, reservation.OperationEvidence{
		TxFailed:                  true,
		TxHash:                    "txhash",
		NullifierUnspentConfirmed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if store.reconcileCalls == 0 {
		t.Fatal("expected Service to use the exported atomic reconciliation command")
	}
	if _, err := svc.Reconcile(ctx, created.ReservationID, reservation.OperationEvidence{NullifierSpent: true}); err != nil {
		t.Fatal(err)
	}
	if len(store.commandKinds) < 2 || store.commandKinds[len(store.commandKinds)-1] != reservation.ReconciliationCommandQuarantineMatchingSpent {
		t.Fatalf("expected exported spent quarantine command, got %v", store.commandKinds)
	}
}
