package reservation

import (
	"context"
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
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Token, StatusReserved, StatusProving); err != nil {
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
