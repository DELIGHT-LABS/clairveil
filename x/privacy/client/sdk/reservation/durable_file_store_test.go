package reservation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurableFileStorePersistsReservationOperationAndLease(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reservation-state.json")
	store, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLeaseForStatus(ctx, created.ReservationID, "worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected state file mode 0600, got %s", info.Mode().Perm())
	}

	reopened, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	reopenedReservation, err := reopened.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if reopenedReservation.Status != StatusProving || reopenedReservation.LeaseToken != lease.Token {
		t.Fatalf("expected reopened proving reservation with lease, got %+v", reopenedReservation)
	}
	reopenedOperation, err := reopened.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if reopenedOperation.Status != OperationStatusPlanned || reopenedOperation.ReservationID != "r1" {
		t.Fatalf("expected reopened linked operation, got %+v", reopenedOperation)
	}
}

func TestDurableFileStoreRollsBackMemoryWhenPersistFails(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reservation-state.json")
	store, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLeaseForStatus(ctx, created.ReservationID, "worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.TransitionWithLease(canceledCtx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err == nil {
		t.Fatal("expected canceled persist to fail")
	}

	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusReserved || reservation.LeaseToken != lease.Token {
		t.Fatalf("expected in-memory state rolled back to reserved lease, got %+v", reservation)
	}
	reopened, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	reopenedReservation, err := reopened.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reopenedReservation.Status != StatusReserved || reopenedReservation.LeaseToken != lease.Token {
		t.Fatalf("expected persisted state to remain reserved lease, got %+v", reopenedReservation)
	}
}

func TestDurableFileStoreOpenMissingPathDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reservation-state.json")
	if _, err := OpenDurableFileStore(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected open to leave missing state path absent, got %v", err)
	}
}

func TestDurableFileStoreStaleEmptyOpenDoesNotClobberCreatedState(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reservation-state.json")
	staleEmptyStore, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}

	creatorStore, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	creatorSvc := Service{Store: creatorStore, Now: fixedNow}
	if _, err := creatorSvc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := staleEmptyStore.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GetReservation(ctx, "r1"); err != nil {
		t.Fatalf("expected created reservation to remain after stale empty refresh: %v", err)
	}
}

func TestDurableFileStoreRejectsStaleCrossProcessMutation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reservation-state.json")
	store, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{Store: store, Now: fixedNow}
	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}

	firstStore, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	secondStore, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	firstSvc := Service{Store: firstStore, Now: fixedNow}
	secondSvc := Service{Store: secondStore, Now: fixedNow}

	firstLease, err := firstSvc.AcquireLeaseForStatus(ctx, created.ReservationID, "worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secondSvc.AcquireLeaseForStatus(ctx, created.ReservationID, "worker-b", StatusReserved, time.Minute); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("expected stale store mutation to observe existing lease, got %v", err)
	}

	reopened, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := reopened.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.LeaseOwner != "worker-a" || reservation.LeaseToken != firstLease.Token {
		t.Fatalf("expected first lease to remain durable, got %+v", reservation)
	}
	staleReservation, err := secondStore.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if staleReservation.LeaseOwner != "worker-a" || staleReservation.LeaseToken != firstLease.Token {
		t.Fatalf("expected stale store memory refresh, got %+v", staleReservation)
	}
}

func TestDurableFileStorePersistsReconcileResult(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reservation-state.json")
	store, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{Store: store, Now: fixedNow}
	_, err = svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:              "op-a",
			ExpectedOutputCommitment: "commitment-a",
			ExpectedDisclosureDigest: "digest-a",
			ExpectedRecipientHash:    "recipient-a",
			ExpectedAmountHash:       "amount-a",
			ExpectedDenom:            "uclair",
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:   true,
		TxHash:           "txhash",
		OutputCommitment: "commitment-a",
		DisclosureDigest: "digest-a",
		RecipientHash:    "recipient-a",
		AmountHash:       "amount-a",
		Denom:            "uclair",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationStatus != OperationStatusSucceeded || result.ReservationStatus != StatusConfirmedSpent {
		t.Fatalf("expected successful reconcile, got %+v", result)
	}

	reopened, err := OpenDurableFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := reopened.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusConfirmedSpent {
		t.Fatalf("expected persisted confirmed reservation, got %s", reservation.Status)
	}
	operation, err := reopened.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusSucceeded {
		t.Fatalf("expected persisted succeeded operation, got %s", operation.Status)
	}
}
