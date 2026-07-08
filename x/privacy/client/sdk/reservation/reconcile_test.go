package reservation

import (
	"context"
	"errors"
	"sync"
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
			BatchItemIndexKnown:      true,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
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
			BatchItemIndexKnown:      true,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
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

func TestServiceReconcileRequiresAuditDigestForOperationSuccess(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:                   "op-a",
			ExpectedOutputCommitment:      "commitment-a",
			ExpectedDisclosureDigest:      "audit-digest-a",
			ExpectedUserDisclosureDigest:  "user-digest-a",
			ExpectedAuditDisclosureDigest: "audit-digest-a",
			ExpectedRecipientHash:         "recipient-a",
			ExpectedAmountHash:            "amount-a",
			ExpectedDenom:                 "uclair",
			BatchItemIndex:                0,
			BatchItemIndexKnown:           true,
			Status:                        OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:       true,
		TxHash:               "txhash",
		OutputCommitment:     "commitment-a",
		UserDisclosureDigest: "user-digest-a",
		RecipientHash:        "recipient-a",
		AmountHash:           "amount-a",
		Denom:                "uclair",
		BatchItemIndex:       0,
		BatchItemIndexKnown:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequiresReview {
		t.Fatalf("expected review when user digest matches but audit digest is missing")
	}
	if result.OperationStatus != OperationStatusConflictSpent {
		t.Fatalf("expected conflict spent, got %s", result.OperationStatus)
	}
}

func TestServiceReconcileMatchesTxHashEvidenceAgainstStoredTxBytesHash(t *testing.T) {
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
			BatchItemIndexKnown:      true,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	markReservationUnknownWithTxBytesHash(t, ctx, svc, "r1", "ABCDEF")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "abcdef",
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
	if result.OperationStatus != OperationStatusSucceeded || result.ReservationStatus != StatusConfirmedSpent {
		t.Fatalf("expected succeeded reconcile, got %+v", result)
	}
}

func TestServiceReconcileTreatsSucceededEvidenceAsKnown(t *testing.T) {
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
			BatchItemIndexKnown:      true,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		TxSucceeded:         true,
		TxHash:              "txhash",
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
	if result.OperationStatus != OperationStatusSucceeded || result.ReservationStatus != StatusConfirmedSpent {
		t.Fatalf("expected succeeded reconcile, got %+v", result)
	}
}

func TestServiceReconcileTreatsFailedEvidenceAsKnown(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID: "op-a",
			Status:      OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{TxFailed: true, TxHash: "txhash"})
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationStatus != OperationStatusFailed || result.ReservationStatus != StatusFailed {
		t.Fatalf("expected failed reconcile, got %+v", result)
	}
}

func TestServiceReconcileRecoversProofReadySpentWithoutTxIdentity(t *testing.T) {
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
			BatchItemIndexKnown:      true,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := markReservationProofReadyForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
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
	if result.ReservationStatus != StatusConfirmedSpent || result.OperationStatus != OperationStatusConflictSpent || !result.RequiresReview {
		t.Fatalf("expected proof-ready spent recovery to close note as conflict, got %+v", result)
	}
	reservation, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusConfirmedSpent {
		t.Fatalf("expected stored reservation confirmed spent, got %s", reservation.Status)
	}
	if reservation.LeaseToken != "" {
		t.Fatalf("expected terminal reconcile to clear proof-ready lease %s, got %s", lease.Token, reservation.LeaseToken)
	}
}

func TestServiceReconcileRecoversProofReadySuccessWhenEvidenceMatches(t *testing.T) {
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
			BatchItemIndexKnown:      true,
			TxBytesHash:              "ABCDEF",
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	markReservationProofReadyForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "abcdef",
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
	if result.ReservationStatus != StatusConfirmedSpent || result.OperationStatus != OperationStatusSucceeded {
		t.Fatalf("expected proof-ready success recovery, got %+v", result)
	}
}

func TestServiceReconcileRequiresMatchingTxEvidenceForFailure(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID: "op-a",
			Status:      OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{TxFailed: true, TxHash: "other-tx"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequiresReview {
		t.Fatalf("expected mismatched failed tx evidence to require review")
	}
	if result.OperationStatus != OperationStatusManualReview || result.ReservationStatus != StatusManualReview {
		t.Fatalf("expected manual review reconcile, got %+v", result)
	}
	reservation, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusManualReview {
		t.Fatalf("expected stored reservation manual review, got %s", reservation.Status)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusManualReview {
		t.Fatalf("expected stored operation manual review, got %s", operation.Status)
	}
}

func TestServiceReconcileAllowsSecondaryReservationForMultiInputOperation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	secondary := testReservation("r2", "note-b", "op-a")
	secondary.NullifierLookupKey = "lookup-b"
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation: &PayrollOperation{
				OperationID:              "op-a",
				ExpectedOutputCommitment: "commitment-a",
				ExpectedDisclosureDigest: "digest-a",
				ExpectedRecipientHash:    "recipient-a",
				ExpectedAmountHash:       "amount-a",
				ExpectedDenom:            "uclair",
				BatchItemIndex:           0,
				BatchItemIndexKnown:      true,
				Status:                   OperationStatusPlanned,
			},
		},
		{Reservation: secondary},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationsForReconcile(t, ctx, svc, "r1", "r2")

	result, err := svc.Reconcile(ctx, "r2", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
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
	if result.OperationStatus != OperationStatusSucceeded {
		t.Fatalf("expected success, got %s", result.OperationStatus)
	}
	reservation, err := store.GetReservation(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusConfirmedSpent {
		t.Fatalf("expected secondary reservation confirmed spent, got %s", reservation.Status)
	}
}

func TestServiceReconcilePreservesTerminalOperationStatusAcrossSiblingReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	secondary := testReservation("r2", "note-b", "op-a")
	secondary.NullifierLookupKey = "lookup-b"
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation: &PayrollOperation{
				OperationID:              "op-a",
				ExpectedOutputCommitment: "commitment-a",
				ExpectedDisclosureDigest: "digest-a",
				ExpectedRecipientHash:    "recipient-a",
				ExpectedAmountHash:       "amount-a",
				ExpectedDenom:            "uclair",
				BatchItemIndex:           0,
				BatchItemIndexKnown:      true,
				Status:                   OperationStatusPlanned,
			},
		},
		{Reservation: secondary},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationsForReconcile(t, ctx, svc, "r1", "r2")

	conflict, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
		OutputCommitment:    "other-commitment",
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
	if conflict.OperationStatus != OperationStatusConflictSpent {
		t.Fatalf("expected initial conflict spent, got %s", conflict.OperationStatus)
	}

	result, err := svc.Reconcile(ctx, "r2", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
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
	if result.OperationStatus != OperationStatusConflictSpent {
		t.Fatalf("expected terminal conflict spent to be preserved, got %s", result.OperationStatus)
	}
	if !result.RequiresReview {
		t.Fatalf("expected preserved terminal operation status to require review")
	}
	if result.Reason != "operation already terminal; preserved existing status" {
		t.Fatalf("unexpected preserved terminal reason %q", result.Reason)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusConflictSpent {
		t.Fatalf("expected stored operation to remain conflict spent, got %s", operation.Status)
	}
	reservation, err := store.GetReservation(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusConfirmedSpent {
		t.Fatalf("expected sibling reservation to reconcile independently, got %s", reservation.Status)
	}
}

func TestServiceReconcileKeepsReviewForFailedReservationWithSucceededOperation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	secondary := testReservation("r2", "note-b", "op-a")
	secondary.NullifierLookupKey = "lookup-b"
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation: &PayrollOperation{
				OperationID:              "op-a",
				ExpectedOutputCommitment: "commitment-a",
				ExpectedDisclosureDigest: "digest-a",
				ExpectedRecipientHash:    "recipient-a",
				ExpectedAmountHash:       "amount-a",
				ExpectedDenom:            "uclair",
				BatchItemIndex:           0,
				BatchItemIndexKnown:      true,
				Status:                   OperationStatusPlanned,
			},
		},
		{Reservation: secondary},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationsForReconcile(t, ctx, svc, "r1", "r2")

	success, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
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
	if success.OperationStatus != OperationStatusSucceeded || success.ReservationStatus != StatusConfirmedSpent {
		t.Fatalf("expected initial success reconcile, got %+v", success)
	}

	failed, err := svc.Reconcile(ctx, "r2", OperationEvidence{
		TxFailed: true,
		TxHash:   "txhash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.OperationStatus != OperationStatusSucceeded || failed.ReservationStatus != StatusManualReview {
		t.Fatalf("expected manual-review reservation with preserved succeeded operation, got %+v", failed)
	}
	if !failed.RequiresReview {
		t.Fatalf("expected first failed sibling reconcile to require review")
	}
	conflicting := testReservation("r3", "note-c", "")
	conflicting.NullifierLookupKey = secondary.NullifierLookupKey
	_, err = svc.Reserve(ctx, ReserveInput{Reservation: conflicting})
	if !errors.Is(err, ErrActiveReservationExists) {
		t.Fatalf("expected manual-review reservation to keep active nullifier lock, got %v", err)
	}

	repeated, err := svc.Reconcile(ctx, "r2", OperationEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.OperationStatus != OperationStatusSucceeded || repeated.ReservationStatus != StatusManualReview {
		t.Fatalf("expected manual-review reservation with succeeded operation, got %+v", repeated)
	}
	if !repeated.RequiresReview {
		t.Fatalf("expected repeated manual-review reservation reconcile to keep review")
	}
	if repeated.Reason != "reservation requires manual review" {
		t.Fatalf("unexpected repeated reconcile reason %q", repeated.Reason)
	}
}

func TestServiceReconcileReturnsTerminalReservationForUnknownEvidence(t *testing.T) {
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
			BatchItemIndexKnown:      true,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")
	success, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
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
	if success.ReservationStatus != StatusConfirmedSpent {
		t.Fatalf("expected confirmed spent setup, got %s", success.ReservationStatus)
	}

	repeated, err := svc.Reconcile(ctx, "r1", OperationEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ReservationStatus != StatusConfirmedSpent {
		t.Fatalf("expected terminal reservation status to be preserved, got %s", repeated.ReservationStatus)
	}
	if repeated.OperationStatus != OperationStatusSucceeded {
		t.Fatalf("expected terminal operation status to be preserved, got %s", repeated.OperationStatus)
	}
	if repeated.RequiresReview {
		t.Fatalf("did not expect review for stable terminal success")
	}
}

func TestServiceReconcileTerminalSpentWithoutOperationRequiresReview(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	conflict, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent: true,
		TxHash:         "txhash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if conflict.ReservationStatus != StatusConfirmedSpent || conflict.OperationStatus != OperationStatusConflictSpent || !conflict.RequiresReview {
		t.Fatalf("expected spent conflict to require review, got %+v", conflict)
	}

	repeated, err := svc.Reconcile(ctx, "r1", OperationEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ReservationStatus != StatusConfirmedSpent {
		t.Fatalf("expected terminal reservation status to be preserved, got %s", repeated.ReservationStatus)
	}
	if repeated.OperationStatus != OperationStatusUnknown {
		t.Fatalf("expected missing operation to report unknown, got %s", repeated.OperationStatus)
	}
	if !repeated.RequiresReview {
		t.Fatalf("expected terminal spent reservation without operation success evidence to require review")
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
			BatchItemIndexKnown:      true,
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
	if !result.RequiresReview {
		t.Fatalf("expected missing batch index evidence to require review")
	}
	if result.OperationStatus != OperationStatusConflictSpent {
		t.Fatalf("expected conflict spent, got %s", result.OperationStatus)
	}
}

func TestServiceReconcileReturnsAtomicUpdateError(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	updateErr := errors.New("reconcile update failed")
	svc := Service{Store: reconcileUpdateFailingStore{Store: store, err: updateErr}, Now: fixedNow}

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
			BatchItemIndexKnown:      true,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	_, err = svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
		OutputCommitment:    "commitment-a",
		DisclosureDigest:    "digest-a",
		RecipientHash:       "recipient-a",
		AmountHash:          "amount-a",
		Denom:               "uclair",
		BatchItemIndex:      0,
		BatchItemIndexKnown: true,
	})
	if !errors.Is(err, updateErr) {
		t.Fatalf("expected reconcile update error, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusSubmitted {
		t.Fatalf("expected reservation to remain Submitted after failed atomic update, got %s", reservation.Status)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status == OperationStatusSucceeded {
		t.Fatalf("failed atomic update should not mark operation succeeded")
	}
}

func TestServiceReconcileRequiresTxEvidenceForSuccess(t *testing.T) {
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
			BatchItemIndexKnown:      true,
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
	if !result.RequiresReview {
		t.Fatalf("expected missing tx evidence to require review")
	}
	if result.OperationStatus != OperationStatusConflictSpent {
		t.Fatalf("expected conflict spent, got %s", result.OperationStatus)
	}
}

func TestServiceReconcileUnknownEvidenceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	first, err := svc.Reconcile(ctx, "r1", OperationEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if first.ReservationStatus != StatusUnknown || first.OperationStatus != OperationStatusUnknown {
		t.Fatalf("expected first reconcile to mark unknown, got %+v", first)
	}
	second, err := svc.Reconcile(ctx, "r1", OperationEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if second.ReservationStatus != StatusUnknown || second.OperationStatus != OperationStatusUnknown || second.RequiresReview {
		t.Fatalf("expected repeated unknown reconcile to remain unknown without review, got %+v", second)
	}
}

func TestServiceReconcilePersistsUnknownOperationStatus(t *testing.T) {
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
			BatchItemIndexKnown:      true,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "r1")

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationStatus != OperationStatusUnknown {
		t.Fatalf("expected unknown operation status, got %s", result.OperationStatus)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusUnknown {
		t.Fatalf("expected persisted unknown operation status, got %s", operation.Status)
	}
}

func TestServiceReconcileRejectsStaleSpentReservationUpdate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	setupSvc := Service{Store: store, Now: fixedNow}

	_, err := setupSvc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:              "op-a",
			ExpectedOutputCommitment: "commitment-a",
			ExpectedDisclosureDigest: "digest-a",
			ExpectedRecipientHash:    "recipient-a",
			ExpectedAmountHash:       "amount-a",
			ExpectedDenom:            "uclair",
			BatchItemIndex:           0,
			BatchItemIndexKnown:      true,
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, setupSvc, "r1")

	racingStore := &reservationRacingStore{Store: store, reservationID: "r1", status: StatusManualReview}
	svc := Service{Store: racingStore, Now: fixedNow}
	_, err = svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
		OutputCommitment:    "commitment-a",
		DisclosureDigest:    "digest-a",
		RecipientHash:       "recipient-a",
		AmountHash:          "amount-a",
		Denom:               "uclair",
		BatchItemIndex:      0,
		BatchItemIndexKnown: true,
	})
	if !errors.Is(err, ErrCompareAndSetFailed) {
		t.Fatalf("expected stale reconcile CAS failure, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusManualReview {
		t.Fatalf("expected concurrent status to remain ManualReview, got %s", reservation.Status)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status == OperationStatusSucceeded {
		t.Fatalf("stale reconcile should not mark operation succeeded")
	}
}

func TestMemoryStoreCompareAndSetReservationStatusWithOperationIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservation := testReservation("r1", "note-a", "op-a")
	reservation.Status = StatusSubmitted
	reservation.CreatedAt = fixedNow()
	reservation.UpdatedAt = fixedNow()
	if _, err := store.CreateReservation(ctx, reservation); err != nil {
		t.Fatal(err)
	}

	_, _, err := store.CompareAndSetReservationStatusWithOperation(ctx, "r1", StatusSubmitted, StatusConfirmedSpent, &PayrollOperation{
		OperationID:   "op-a",
		ReservationID: "r1",
		Status:        OperationStatusSucceeded,
		UpdatedAt:     fixedNow(),
	}, fixedNow())
	if !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("expected operation-not-found error, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusSubmitted {
		t.Fatalf("expected reservation to remain Submitted when operation update cannot be applied, got %s", unchanged.Status)
	}
}

func submitReservationForReconcile(t *testing.T, ctx context.Context, svc Service, reservationID string) {
	t.Helper()
	submitReservationsForReconcile(t, ctx, svc, reservationID)
}

func markReservationUnknownWithTxBytesHash(t *testing.T, ctx context.Context, svc Service, reservationID string, txBytesHash string) {
	t.Helper()

	lease := markReservationProofReadyForReconcile(t, ctx, svc, reservationID)
	if _, _, err := svc.MarkBroadcastUnknownBatch(ctx, []SubmittedReservationRef{{
		ReservationID: reservationID,
		LeaseToken:    lease.Token,
	}}, nil, BroadcastAttemptUpdate{TxBytesHash: txBytesHash}); err != nil {
		t.Fatal(err)
	}
}

func markReservationProofReadyForReconcile(t *testing.T, ctx context.Context, svc Service, reservationID string) *Lease {
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
	return lease
}

func submitReservationsForReconcile(t *testing.T, ctx context.Context, svc Service, reservationIDs ...string) {
	t.Helper()

	refs := make([]SubmittedReservationRef, 0, len(reservationIDs))
	for _, reservationID := range reservationIDs {
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
		refs = append(refs, SubmittedReservationRef{ReservationID: reservationID, LeaseToken: lease.Token})
	}
	if _, _, err := svc.MarkSubmittedBatch(ctx, refs, nil, SubmittedReservationUpdate{TxHash: "txhash", TxBytesHash: "tx-bytes", SignDocHash: "sign-doc", AccountSequence: 1}); err != nil {
		t.Fatal(err)
	}
}

type reconcileUpdateFailingStore struct {
	Store
	err error
}

func (s reconcileUpdateFailingStore) CompareAndSetReservationStatusWithOperation(context.Context, string, ReservationStatus, ReservationStatus, *PayrollOperation, time.Time) (*NoteReservation, *PayrollOperation, error) {
	return nil, nil, s.err
}

type reservationRacingStore struct {
	Store
	reservationID string
	status        ReservationStatus
	once          sync.Once
}

func (s *reservationRacingStore) GetReservation(ctx context.Context, reservationID string) (*NoteReservation, error) {
	reservation, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	if reservationID == s.reservationID {
		s.once.Do(func() {
			stale := *reservation
			stale.Status = s.status
			stale.UpdatedAt = fixedNow()
			_, _ = s.Store.UpdateReservation(ctx, stale)
		})
	}
	return reservation, nil
}
