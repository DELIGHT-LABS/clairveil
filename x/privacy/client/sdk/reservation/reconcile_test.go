package reservation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceReconcileRequiresOperationEvidenceForSuccess(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:              "op-a",
			ExpectedOutputCommitment: "commitment-a",
			ExpectedDisclosureDigest: "digest-a",
			ExpectedRecipientHash:    "recipient-a",
			ExpectedAmountHash:       "amount-a",
			ExpectedDenom:            "uclair",
			BatchItemIndex:           0,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		OutputCommitment:    "other-commitment",
		Denom:               "uclair",
		BatchItemIndex:      0,
		BatchItemIndexKnown: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequiresReview {
		t.Fatalf("expected review for mismatched spent evidence")
	}
	if result.OperationStatus != OperationStatusConflictSpent {
		t.Fatalf("expected conflict spent, got %s", result.OperationStatus)
	}
}

func TestServiceReconcileMarksSuccessWhenEvidenceMatches(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:              "op-a",
			ExpectedOutputCommitment: "commitment-a",
			ExpectedDisclosureDigest: "digest-a",
			ExpectedRecipientHash:    "recipient-a",
			ExpectedAmountHash:       "amount-a",
			ExpectedDenom:            "uclair",
			BatchItemIndex:           0,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		OutputCommitment:    "commitment-a",
		DisclosureDigest:    "digest-a",
		RecipientHash:       "recipient-a",
		AmountHash:          "amount-a",
		Denom:               "uclair",
		BatchItemIndex:      0,
		BatchItemIndexKnown: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequiresReview {
		t.Fatalf("did not expect review")
	}
	if result.OperationStatus != OperationStatusSucceeded {
		t.Fatalf("expected success, got %s", result.OperationStatus)
	}
}

func TestServiceReconcileRequiresBatchItemIndexEvidence(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:              "op-a",
			ExpectedOutputCommitment: "commitment-a",
			ExpectedDisclosureDigest: "digest-a",
			ExpectedRecipientHash:    "recipient-a",
			ExpectedAmountHash:       "amount-a",
			ExpectedDenom:            "uclair",
			BatchItemIndex:           0,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:   true,
		OutputCommitment: "commitment-a",
		DisclosureDigest: "digest-a",
		RecipientHash:    "recipient-a",
		AmountHash:       "amount-a",
		Denom:            "uclair",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequiresReview {
		t.Fatalf("expected missing batch index evidence to require review")
	}
	if result.OperationStatus != OperationStatusConflictSpent {
		t.Fatalf("expected conflict spent, got %s", result.OperationStatus)
	}
}

func TestServiceReconcileReturnsOperationUpdateError(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	updateErr := errors.New("operation update failed")
	svc := Service{Store: operationUpdateFailingStore{Store: store, err: updateErr}, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:              "op-a",
			ExpectedOutputCommitment: "commitment-a",
			ExpectedDisclosureDigest: "digest-a",
			ExpectedRecipientHash:    "recipient-a",
			ExpectedAmountHash:       "amount-a",
			ExpectedDenom:            "uclair",
			BatchItemIndex:           0,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	_, err = svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		OutputCommitment:    "commitment-a",
		DisclosureDigest:    "digest-a",
		RecipientHash:       "recipient-a",
		AmountHash:          "amount-a",
		Denom:               "uclair",
		BatchItemIndex:      0,
		BatchItemIndexKnown: true,
	})
	if !errors.Is(err, updateErr) {
		t.Fatalf("expected operation update error, got %v", err)
	}
}

func submitReservationForReconcile(t *testing.T, ctx context.Context, svc Service, reservationID string) {
	t.Helper()

	lease, err := svc.AcquireLease(ctx, reservationID, "reconcile-test-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, reservationID, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, reservationID, lease.Token, StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkSubmitted(ctx, reservationID, lease.Token, "txhash", "tx-bytes", "sign-doc", 1); err != nil {
		t.Fatal(err)
	}
}

type operationUpdateFailingStore struct {
	Store
	err error
}

func (s operationUpdateFailingStore) UpdateOperation(context.Context, PayrollOperation) (*PayrollOperation, error) {
	return nil, s.err
}
