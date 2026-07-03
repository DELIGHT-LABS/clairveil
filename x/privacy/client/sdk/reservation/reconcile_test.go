package reservation

import (
	"context"
	"testing"
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
	if _, err := svc.Transition(ctx, "r1", StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, "r1", StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, "r1", StatusProofReady, StatusSubmitted); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:   true,
		OutputCommitment: "other-commitment",
		Denom:            "uclair",
		BatchItemIndex:   0,
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
	if _, err := svc.Transition(ctx, "r1", StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, "r1", StatusProving, StatusProofReady); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, "r1", StatusProofReady, StatusSubmitted); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:   true,
		OutputCommitment: "commitment-a",
		DisclosureDigest: "digest-a",
		RecipientHash:    "recipient-a",
		AmountHash:       "amount-a",
		Denom:            "uclair",
		BatchItemIndex:   0,
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
