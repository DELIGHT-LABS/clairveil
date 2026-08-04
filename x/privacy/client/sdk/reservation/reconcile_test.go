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

func TestTxEvidenceMatchesOperationRequiresMatchingFields(t *testing.T) {
	operation := &PayrollOperation{
		TxHash:      "tx-hash",
		TxBytesHash: "tx-bytes-hash",
		SignDocHash: "sign-doc-hash",
	}
	if txEvidenceMatchesOperation(operation, OperationEvidence{TxHash: "tx-bytes-hash"}) {
		t.Fatal("tx hash must not match a stored tx-bytes hash")
	}
	if txEvidenceMatchesOperation(operation, OperationEvidence{TxBytesHash: "tx-hash"}) {
		t.Fatal("tx-bytes hash must not match a stored tx hash")
	}
	if txEvidenceMatchesOperation(operation, OperationEvidence{SignDocHash: "sign-doc-hash"}) {
		t.Fatal("sign-doc hash alone must not satisfy chain inclusion identity")
	}
	if !txEvidenceMatchesOperation(operation, OperationEvidence{TxHash: "TX-HASH", SignDocHash: "SIGN-DOC-HASH"}) {
		t.Fatal("matching tx hash and compatible sign-doc hash should satisfy tx identity")
	}
	if txEvidenceMatchesOperation(&PayrollOperation{TxBytesHash: "tx-bytes-hash"}, OperationEvidence{
		TxHash:      "unexpected-tx-hash",
		TxBytesHash: "tx-bytes-hash",
	}) {
		t.Fatal("an unexpected tx hash must conflict even when tx-bytes hash matches")
	}
	if !txEvidenceMatchesOperation(&PayrollOperation{TxBytesHash: "0xABCDEF"}, OperationEvidence{TxBytesHash: "abcdef"}) {
		t.Fatal("optional 0x prefix and hex case must be normalized consistently")
	}
}

func TestServiceReconcileTerminalSuccessAuditsConflictingTxIdentity(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}

	repeated, err := svc.Reconcile(ctx, "r1", OperationEvidence{TxHash: "different-tx"})
	if err != nil {
		t.Fatal(err)
	}
	if !repeated.RequiresReview || repeated.Reason != "terminal reservation conflicts with transaction identity evidence" {
		t.Fatalf("expected terminal transaction identity conflict review, got %+v", repeated)
	}
	stored, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ReconciliationReviewReason != repeated.Reason {
		t.Fatalf("expected terminal identity conflict audit, got %q", stored.ReconciliationReviewReason)
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

func TestServiceReconcileMatchesTxBytesEvidenceAgainstStoredTxBytesHash(t *testing.T) {
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
		TxBytesHash:         "abcdef",
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

func TestServiceReconcileConflictingExecutionEvidenceNeverSucceeds(t *testing.T) {
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
		TxFailed:            true,
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
	if !result.RequiresReview || result.ReservationStatus != StatusConfirmedSpent || result.OperationStatus != OperationStatusConflictSpent {
		t.Fatalf("expected conflicting execution evidence to become ConflictSpent, got %+v", result)
	}
	if result.Reason != "conflicting transaction execution evidence" {
		t.Fatalf("expected conflicting execution audit reason, got %+v", result)
	}
}

func TestServiceReconcileConflictingExecutionEvidenceLocksProofReadyOperation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, _ := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	result, err := svc.Reconcile(ctx, created.ReservationID, OperationEvidence{
		TxSucceeded:               true,
		TxFailed:                  true,
		NullifierUnspentConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequiresReview || result.ReservationStatus != StatusManualReview || result.OperationStatus != OperationStatusManualReview {
		t.Fatalf("expected conflicting execution evidence to lock the operation, got %+v", result)
	}
	stored, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusManualReview || stored.LeaseToken != "" || !stored.LeaseUntil.IsZero() {
		t.Fatalf("conflicting execution evidence left broadcast authority active: %+v", stored)
	}
}

func TestServiceReconcileNormalizesConflictingTransactionAndNullifierEvidence(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		evidence          OperationEvidence
		reservationStatus ReservationStatus
		operationStatus   OperationStatus
	}{
		{
			name: "successful tx with confirmed unspent nullifier requires review",
			evidence: OperationEvidence{
				TxSucceeded:               true,
				NullifierUnspentConfirmed: true,
			},
			reservationStatus: StatusManualReview,
			operationStatus:   OperationStatusManualReview,
		},
		{
			name: "failed tx with spent nullifier is conflict spent",
			evidence: OperationEvidence{
				TxFailed:       true,
				NullifierSpent: true,
			},
			reservationStatus: StatusConfirmedSpent,
			operationStatus:   OperationStatusConflictSpent,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
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

			result, err := svc.Reconcile(ctx, "r1", testCase.evidence)
			if err != nil {
				t.Fatal(err)
			}
			if !result.RequiresReview || result.ReservationStatus != testCase.reservationStatus || result.OperationStatus != testCase.operationStatus {
				t.Fatalf("unexpected conflicting evidence result: %+v", result)
			}
		})
	}
}

func TestConflictingNullifierEvidenceAtomicallyRevokesProofReadyOperation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	second := testReservation("r2", "note-b", "op-a")
	second.NullifierLookupKey = "lookup-b"
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{Reservation: second},
	})
	if err != nil {
		t.Fatal(err)
	}
	refs, _, err := svc.BeginProvingOperation(ctx, "op-a", []string{"r1", "r2"}, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{
		OperationID: "op-a",
		PayloadHash: "payload-a",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:            true,
		NullifierUnspentConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReservationStatus != StatusManualReview || result.OperationStatus != OperationStatusManualReview || !result.RequiresReview {
		t.Fatalf("unexpected conflicting nullifier result: %+v", result)
	}
	for _, id := range []string{"r1", "r2"} {
		reservation, err := store.GetReservation(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if reservation.Status != StatusManualReview || reservation.LeaseToken != "" || reservation.LeaseOwner != "" {
			t.Fatalf("expected %s authority to be revoked atomically, got %+v", id, reservation)
		}
	}
}

func TestOperationReconciliationCannotCreateSecondActiveReservationForSameNote(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	oldReservation := testReservation("old", "note-a", "old-op")
	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: oldReservation,
		Operation:   &PayrollOperation{OperationID: "old-op", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, "old", StatusReserved, StatusReplanRequired); err != nil {
		t.Fatal(err)
	}
	newReservation := testReservation("new", "note-a", "new-op")
	newReservation.NullifierLookupKey = oldReservation.NullifierLookupKey
	if _, err := svc.Reserve(ctx, ReserveInput{
		Reservation: newReservation,
		Operation:   &PayrollOperation{OperationID: "new-op", Status: OperationStatusPlanned},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Reconcile(ctx, "old", OperationEvidence{
		NullifierSpent:            true,
		NullifierUnspentConfirmed: true,
	})
	if !errors.Is(err, ErrActiveReservationExists) {
		t.Fatalf("expected operation reconciliation active-key conflict, got %v", err)
	}
	oldStored, _ := store.GetReservation(ctx, "old")
	newStored, _ := store.GetReservation(ctx, "new")
	if oldStored.Status != StatusReplanRequired || newStored.Status != StatusReserved {
		t.Fatalf("rejected operation transition partially mutated reservations: old=%s new=%s", oldStored.Status, newStored.Status)
	}
}

func TestServiceReconcileAuditsConflictingEvidenceForTerminalReservations(t *testing.T) {
	for _, testCase := range []struct {
		name                string
		initialEvidence     OperationEvidence
		conflictingEvidence OperationEvidence
		status              ReservationStatus
	}{
		{
			name: "failed reservation later reports spent nullifier",
			initialEvidence: OperationEvidence{
				TxFailed:                  true,
				TxHash:                    "txhash",
				NullifierUnspentConfirmed: true,
			},
			conflictingEvidence: OperationEvidence{NullifierSpent: true},
			status:              StatusConfirmedSpent,
		},
		{
			name: "confirmed spent reservation later reports failed and unspent",
			initialEvidence: OperationEvidence{
				NullifierSpent: true,
				TxHash:         "txhash",
			},
			conflictingEvidence: OperationEvidence{
				TxFailed:                  true,
				NullifierUnspentConfirmed: true,
			},
			status: StatusConfirmedSpent,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
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
			if _, err := svc.Reconcile(ctx, "r1", testCase.initialEvidence); err != nil {
				t.Fatal(err)
			}

			result, err := svc.Reconcile(ctx, "r1", testCase.conflictingEvidence)
			if err != nil {
				t.Fatal(err)
			}
			if !result.RequiresReview || result.ReservationStatus != testCase.status {
				t.Fatalf("expected terminal conflict review, got %+v", result)
			}
			stored, err := store.GetReservation(ctx, "r1")
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != testCase.status || stored.ReconciliationReviewReason == "" || stored.LastReconciledAt.IsZero() {
				t.Fatalf("expected terminal audit fields to persist, got %+v", stored)
			}
		})
	}
}

func TestServiceReconcileQuarantinesSpentNoteAcrossFailedAndSiblingReservations(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	if _, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("failed", "note-a", "op-failed"), Operation: &PayrollOperation{OperationID: "op-failed", Status: OperationStatusPlanned}}); err != nil {
		t.Fatal(err)
	}
	submitReservationForReconcile(t, ctx, svc, "failed")
	if _, err := svc.Reconcile(ctx, "failed", OperationEvidence{TxFailed: true, TxHash: "txhash", NullifierUnspentConfirmed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, "failed", StatusFailed, StatusReplanRequired); err != nil {
		t.Fatal(err)
	}
	// Simulate a stale historical operation whose inactive reservation still
	// shares this note. Quarantine must update it too, not only the operation
	// named by the current reconcile call.
	if _, err := svc.Reserve(ctx, ReserveInput{Reservation: NoteReservation{
		ReservationID:      "stale",
		NoteID:             "note-a-stale",
		OwnerKeyID:         "owner-a",
		NullifierLookupKey: "lookup-a",
		OperationID:        "op-stale",
		Status:             StatusReserved,
	}, Operation: &PayrollOperation{OperationID: "op-stale", Status: OperationStatusPlanned}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, "stale", StatusReserved, StatusReplanRequired); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reserve(ctx, ReserveInput{Reservation: NoteReservation{
		ReservationID:      "sibling",
		NoteID:             "note-a-retry",
		OwnerKeyID:         "owner-a",
		NullifierLookupKey: "lookup-a",
		OperationID:        "op-sibling",
		Status:             StatusReserved,
	}, Operation: &PayrollOperation{
		OperationID:              "op-sibling",
		Status:                   OperationStatusPlanned,
		ExpectedOutputCommitment: "output-a",
		ExpectedDisclosureDigest: "digest-a",
		ExpectedRecipientHash:    "recipient-a",
		ExpectedAmountHash:       "amount-a",
		ExpectedDenom:            "uclair",
		BatchItemIndex:           0,
		BatchItemIndexKnown:      true,
	}}); err != nil {
		t.Fatal(err)
	}

	submitReservationForReconcile(t, ctx, svc, "sibling")
	result, err := svc.Reconcile(ctx, "sibling", OperationEvidence{
		TxSucceeded:         true,
		TxHash:              "txhash",
		TxBytesHash:         "tx-bytes",
		SignDocHash:         "sign-doc",
		NullifierSpent:      true,
		OutputCommitment:    "output-a",
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
	if result.ReservationStatus != StatusConfirmedSpent || result.RequiresReview {
		t.Fatalf("expected successful current operation to quarantine spent inventory without review, got %+v", result)
	}
	for _, reservationID := range []string{"failed", "stale", "sibling"} {
		stored, err := store.GetReservation(ctx, reservationID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != StatusConfirmedSpent {
			t.Fatalf("expected %s to be quarantined, got %+v", reservationID, stored)
		}
	}
	failedOperation, err := store.GetOperation(ctx, "op-failed")
	if err != nil {
		t.Fatal(err)
	}
	if failedOperation.Status != OperationStatusFailed {
		t.Fatalf("expected prior terminal operation to remain unchanged, got %+v", failedOperation)
	}
	siblingOperation, err := store.GetOperation(ctx, "op-sibling")
	if err != nil {
		t.Fatal(err)
	}
	if siblingOperation.Status != OperationStatusSucceeded {
		t.Fatalf("expected current operation to retain its success result, got %+v", siblingOperation)
	}
	staleOperation, err := store.GetOperation(ctx, "op-stale")
	if err != nil {
		t.Fatal(err)
	}
	if staleOperation.Status != OperationStatusConflictSpent {
		t.Fatalf("expected stale sibling operation to be quarantined, got %+v", staleOperation)
	}
	if _, err := svc.Transition(ctx, "failed", StatusConfirmedSpent, StatusReplanRequired); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected confirmed spent reservation to remain terminal, got %v", err)
	}
	if _, err := svc.Reserve(ctx, ReserveInput{Reservation: NoteReservation{
		ReservationID:      "new-attempt",
		NoteID:             "note-a-new",
		OwnerKeyID:         "owner-a",
		NullifierLookupKey: "lookup-a",
		Status:             StatusReserved,
	}}); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected confirmed spent note to reject new reservation, got %v", err)
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

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		TxFailed:                  true,
		TxHash:                    "txhash",
		NullifierUnspentConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationStatus != OperationStatusFailed || result.ReservationStatus != StatusFailed {
		t.Fatalf("expected failed reconcile, got %+v", result)
	}
}

func TestServiceReconcileDoesNotReuseFailedTransactionWithoutConfirmedUnspentNullifier(t *testing.T) {
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

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{TxFailed: true, TxHash: "txhash"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequiresReview || result.ReservationStatus != StatusSubmitted || result.OperationStatus != OperationStatusSubmitted {
		t.Fatalf("expected failed tx without an unspent confirmation to remain locked, got %+v", result)
	}
	stored, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusSubmitted {
		t.Fatalf("expected Submitted reservation to remain locked, got %s", stored.Status)
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
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, "r1", "reconcile-test-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{{
		ReservationID: "r1",
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, ProofReadyOperationUpdate{
		OperationID: "op-a",
		PayloadHash: "payload-op-a",
		TxBytesHash: "ABCDEF",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		NullifierSpent:      true,
		TxBytesHash:         "abcdef",
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

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		TxFailed:                  true,
		TxHash:                    "other-tx",
		NullifierUnspentConfirmed: true,
	})
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

	evidence := OperationEvidence{
		NullifierSpent:      true,
		TxHash:              "txhash",
		OutputCommitment:    "commitment-a",
		DisclosureDigest:    "digest-a",
		RecipientHash:       "recipient-a",
		AmountHash:          "amount-a",
		Denom:               "uclair",
		BatchItemIndex:      0,
		BatchItemIndexKnown: true,
	}
	result, err := svc.Reconcile(ctx, "r2", evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequiresReview || result.OperationStatus == OperationStatusSucceeded {
		t.Fatalf("expected single-input reconcile to refuse multi-input success, got %+v", result)
	}
	_, err = svc.ReconcileOperation(ctx, "op-a", []OperationReservationEvidence{
		{ReservationID: "r1", Evidence: evidence},
		{ReservationID: "r2", Evidence: evidence},
	})
	if !errors.Is(err, ErrManualReviewRequired) {
		t.Fatalf("expected missing transaction-success evidence to be rejected, got %v", err)
	}
	evidence.TxSucceeded = true
	result, err = svc.ReconcileOperation(ctx, "op-a", []OperationReservationEvidence{
		{ReservationID: "r1", Evidence: evidence},
		{ReservationID: "r2", Evidence: evidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationStatus != OperationStatusSucceeded || result.RequiresReview {
		t.Fatalf("expected complete operation success, got %+v", result)
	}
	retried, err := svc.ReconcileOperation(ctx, "op-a", []OperationReservationEvidence{
		{ReservationID: "r1", Evidence: evidence},
		{ReservationID: "r2", Evidence: evidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if retried.OperationStatus != OperationStatusSucceeded || retried.RequiresReview {
		t.Fatalf("expected idempotent operation reconciliation, got %+v", retried)
	}
	for _, reservationID := range []string{"r1", "r2"} {
		reservation, getErr := store.GetReservation(ctx, reservationID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if reservation.Status != StatusConfirmedSpent {
			t.Fatalf("expected %s confirmed spent, got %s", reservationID, reservation.Status)
		}
	}
	conflict, err := svc.Reconcile(ctx, "r1", OperationEvidence{
		TxFailed:                  true,
		TxHash:                    "txhash",
		NullifierUnspentConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !conflict.RequiresReview || conflict.ReservationStatus != StatusConfirmedSpent {
		t.Fatalf("expected terminal multi-input conflict audit, got %+v", conflict)
	}
	stored, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ReconciliationReviewReason == "" || stored.LastReconciledAt.IsZero() {
		t.Fatalf("expected terminal multi-input audit fields to persist, got %+v", stored)
	}
}

func TestServiceReconcileOperationAtomicallyFailsEveryInput(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	secondary := testReservation("r2", "note-b", "op-a")
	secondary.NullifierLookupKey = "lookup-b"
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{Reservation: secondary},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationsForReconcile(t, ctx, svc, "r1", "r2")

	evidence := OperationEvidence{
		TxFailed:                  true,
		TxHash:                    "txhash",
		NullifierUnspentConfirmed: true,
	}
	single, err := svc.Reconcile(ctx, "r1", evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !single.RequiresReview || single.ReservationStatus != StatusSubmitted {
		t.Fatalf("expected singular multi-input failure reconciliation to remain unchanged, got %+v", single)
	}
	for _, reservationID := range []string{"r1", "r2"} {
		reservation, getErr := store.GetReservation(ctx, reservationID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if reservation.Status != StatusSubmitted {
			t.Fatalf("expected %s to remain Submitted, got %s", reservationID, reservation.Status)
		}
	}

	result, err := svc.ReconcileOperation(ctx, "op-a", []OperationReservationEvidence{
		{ReservationID: "r1", Evidence: evidence},
		{ReservationID: "r2", Evidence: evidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequiresReview || result.ReservationStatus != StatusFailed || result.OperationStatus != OperationStatusFailed {
		t.Fatalf("expected atomic failed operation reconciliation, got %+v", result)
	}
	for _, reservationID := range []string{"r1", "r2"} {
		reservation, getErr := store.GetReservation(ctx, reservationID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if reservation.Status != StatusFailed {
			t.Fatalf("expected %s Failed, got %s", reservationID, reservation.Status)
		}
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusFailed {
		t.Fatalf("expected operation Failed, got %s", operation.Status)
	}

	retried, err := svc.ReconcileOperation(ctx, "op-a", []OperationReservationEvidence{
		{ReservationID: "r1", Evidence: evidence},
		{ReservationID: "r2", Evidence: evidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if retried.RequiresReview || retried.OperationStatus != OperationStatusFailed {
		t.Fatalf("expected idempotent failed operation reconciliation, got %+v", retried)
	}
}

func TestServiceReconcileOperationRejectsPartialEvidenceWithoutMutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	secondary := testReservation("r2", "note-b", "op-a")
	secondary.NullifierLookupKey = "lookup-b"
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{Reservation: secondary},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitReservationsForReconcile(t, ctx, svc, "r1", "r2")

	_, err = svc.ReconcileOperation(ctx, "op-a", []OperationReservationEvidence{{
		ReservationID: "r1",
		Evidence:      OperationEvidence{NullifierSpent: true, TxHash: "txhash"},
	}})
	if !errors.Is(err, ErrManualReviewRequired) {
		t.Fatalf("expected partial evidence rejection, got %v", err)
	}
	for _, reservationID := range []string{"r1", "r2"} {
		reservation, getErr := store.GetReservation(ctx, reservationID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if reservation.Status != StatusSubmitted {
			t.Fatalf("expected %s to remain Submitted after rejected operation reconcile, got %s", reservationID, reservation.Status)
		}
	}
}

func TestServiceReconcileKnownPendingAttemptMovesProofReadyToUnknown(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")
	ref := SubmittedReservationRef{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}
	if _, _, err := svc.MarkBroadcastAttempting(ctx, []SubmittedReservationRef{ref}, []string{"op-a"}, BroadcastAttemptStart{Reason: "test", TxHash: "0xABC"}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Reconcile(ctx, "r1", OperationEvidence{TxKnown: true, TxHash: "0xABC"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReservationStatus != StatusUnknown || result.OperationStatus != OperationStatusUnknown || result.RequiresReview {
		t.Fatalf("unexpected pending transaction reconciliation: %+v", result)
	}
	stored, _ := store.GetReservation(ctx, "r1")
	if stored.Status != StatusUnknown || normalizedTxIdentity(stored.TxHash) != "abc" || stored.BroadcastInFlight || stored.LeaseToken != "" {
		t.Fatalf("pending transaction identity was not durably recorded: %+v", stored)
	}
	operation, _ := store.GetOperation(ctx, "op-a")
	if operation.Status != OperationStatusUnknown || normalizedTxIdentity(operation.TxHash) != "abc" {
		t.Fatalf("pending operation identity was not durably recorded: %+v", operation)
	}
}

func TestServiceReconcileKnownPendingOperationMovesEveryInputAtomically(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	second := testReservation("r2", "note-b", "op-a")
	second.NullifierLookupKey = "lookup-b"
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{Reservation: second},
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := beginProvingOperationForTest(t, ctx, svc, "op-a", "r1", "r2")
	if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-a"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkBroadcastAttempting(ctx, refs, []string{"op-a"}, BroadcastAttemptStart{Reason: "test", TxBytesHash: "cafe"}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.ReconcileOperation(ctx, "op-a", []OperationReservationEvidence{
		{ReservationID: "r1", Evidence: OperationEvidence{TxKnown: true, TxBytesHash: "0xCAFE"}},
		{ReservationID: "r2", Evidence: OperationEvidence{TxKnown: true, TxBytesHash: "cafe"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReservationStatus != StatusUnknown || result.OperationStatus != OperationStatusUnknown || result.RequiresReview {
		t.Fatalf("unexpected pending operation reconciliation: %+v", result)
	}
	for _, id := range []string{"r1", "r2"} {
		stored, getErr := store.GetReservation(ctx, id)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if stored.Status != StatusUnknown || stored.TxBytesHash != "cafe" || stored.BroadcastInFlight || stored.LeaseToken != "" {
			t.Fatalf("pending operation input %s was not atomically reconciled: %+v", id, stored)
		}
	}
}

func TestServiceReconcileKnownPendingOperationRequiresSharedTransactionIdentity(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	second := testReservation("r2", "note-b", "op-a")
	second.NullifierLookupKey = "lookup-b"
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{Reservation: second},
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := beginProvingOperationForTest(t, ctx, svc, "op-a", "r1", "r2")
	if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-a"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkBroadcastAttempting(ctx, refs, []string{"op-a"}, BroadcastAttemptStart{Reason: "test", TxHash: "tx-a"}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.ReconcileOperation(ctx, "op-a", []OperationReservationEvidence{
		{ReservationID: "r1", Evidence: OperationEvidence{TxKnown: true, TxHash: "tx-a"}},
		{ReservationID: "r2", Evidence: OperationEvidence{TxKnown: true, TxBytesHash: "tx-bytes-b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequiresReview || result.Reason != "operation inputs do not share a common pending tx identity" {
		t.Fatalf("expected disjoint transaction identities to require review, got %+v", result)
	}
	for _, id := range []string{"r1", "r2"} {
		stored, getErr := store.GetReservation(ctx, id)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if stored.Status != StatusProofReady || stored.TxHash != "tx-a" || stored.TxBytesHash != "" {
			t.Fatalf("disjoint identity evidence mutated %s: %+v", id, stored)
		}
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
	if success.OperationStatus != OperationStatusSucceeded || success.ReservationStatus != StatusConfirmedSpent {
		t.Fatalf("expected initial success reconcile, got %+v", success)
	}
	secondary := testReservation("r2", "note-b", "op-a")
	secondary.NullifierLookupKey = "lookup-b"
	secondary.Status = StatusSubmitted
	secondary.TxHash = "txhash"
	secondary.CreatedAt = fixedNow()
	secondary.UpdatedAt = fixedNow()
	if _, err := store.unsafeImportReservationForTesting(ctx, secondary); err != nil {
		t.Fatal(err)
	}

	failed, err := svc.Reconcile(ctx, "r2", OperationEvidence{
		TxFailed:                  true,
		TxHash:                    "txhash",
		NullifierUnspentConfirmed: true,
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

func TestServiceReconcileAuditsFailedReceiptAgainstTerminalSuccessForEveryNullifierState(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		evidence   OperationEvidence
		wantReason string
	}{
		{
			name: "unknown nullifier state",
			evidence: OperationEvidence{
				TxFailed: true,
				TxHash:   "txhash",
			},
			wantReason: "terminal spent reservation conflicts with failed transaction evidence",
		},
		{
			name: "spent nullifier",
			evidence: OperationEvidence{
				TxFailed:       true,
				TxHash:         "txhash",
				NullifierSpent: true,
			},
			wantReason: "terminal spent reservation conflicts with failed transaction and spent nullifier evidence",
		},
		{
			name: "confirmed unspent nullifier",
			evidence: OperationEvidence{
				TxFailed:                  true,
				TxHash:                    "txhash",
				NullifierUnspentConfirmed: true,
			},
			wantReason: "terminal spent reservation conflicts with failed transaction and unspent nullifier evidence",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
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
			if _, err := svc.Reconcile(ctx, "r1", OperationEvidence{
				NullifierSpent:      true,
				TxHash:              "txhash",
				OutputCommitment:    "commitment-a",
				DisclosureDigest:    "digest-a",
				RecipientHash:       "recipient-a",
				AmountHash:          "amount-a",
				Denom:               "uclair",
				BatchItemIndex:      0,
				BatchItemIndexKnown: true,
			}); err != nil {
				t.Fatal(err)
			}

			result, err := svc.Reconcile(ctx, "r1", testCase.evidence)
			if err != nil {
				t.Fatal(err)
			}
			if !result.RequiresReview || result.ReservationStatus != StatusConfirmedSpent || result.OperationStatus != OperationStatusSucceeded {
				t.Fatalf("expected terminal success to be preserved with review, got %+v", result)
			}
			if result.Reason != testCase.wantReason {
				t.Fatalf("unexpected review reason %q", result.Reason)
			}
			stored, err := store.GetReservation(ctx, "r1")
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != StatusConfirmedSpent || stored.ReconciliationReviewReason != testCase.wantReason || stored.LastReconciledAt.IsZero() {
				t.Fatalf("expected terminal conflict audit to persist, got %+v", stored)
			}
		})
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

func TestMemoryStoreInternalReconcileTransitionIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservation := testReservation("r1", "note-a", "op-a")
	reservation.Status = StatusSubmitted
	reservation.CreatedAt = fixedNow()
	reservation.UpdatedAt = fixedNow()
	if _, err := store.unsafeImportReservationForTesting(ctx, reservation); err != nil {
		t.Fatal(err)
	}

	_, _, err := store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID: "r1",
		From:          StatusSubmitted,
		To:            StatusConfirmedSpent,
		Operation: &PayrollOperation{
			OperationID:   "op-a",
			ReservationID: "r1",
			Status:        OperationStatusSucceeded,
			UpdatedAt:     fixedNow(),
		},
		Now:               fixedNow(),
		serviceAuthorized: true,
	})
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

func TestMemoryStoreRejectsUnsealedReconciliationTransition(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservation := testReservation("r1", "note-a", "")
	if _, err := store.CreateReservation(ctx, reservation); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID: "r1",
		From:          StatusReserved,
		To:            StatusManualReview,
		Now:           fixedNow(),
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected unsealed reconciliation transition rejection, got %v", err)
	}
}

func submitReservationForReconcile(t *testing.T, ctx context.Context, svc Service, reservationID string) {
	t.Helper()
	submitReservationsForReconcile(t, ctx, svc, reservationID)
}

func markReservationUnknownWithTxBytesHash(t *testing.T, ctx context.Context, svc Service, reservationID string, txBytesHash string) {
	t.Helper()

	lease := markReservationProofReadyForReconcile(t, ctx, svc, reservationID)
	refs := []SubmittedReservationRef{{
		ReservationID: reservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}
	if _, _, err := svc.MarkBroadcastAttempting(ctx, refs, nil, BroadcastAttemptStart{TxBytesHash: txBytesHash}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkBroadcastUnknownBatch(ctx, refs, nil, BroadcastAttemptUpdate{TxBytesHash: txBytesHash}); err != nil {
		t.Fatal(err)
	}
}

func markReservationProofReadyForReconcile(t *testing.T, ctx context.Context, svc Service, reservationID string) *Lease {
	t.Helper()

	lease, err := svc.AcquireLease(ctx, reservationID, "reconcile-test-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, reservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	markLeasedReservationProofReady(t, ctx, svc, reservationID, lease)
	return lease
}

func submitReservationsForReconcile(t *testing.T, ctx context.Context, svc Service, reservationIDs ...string) {
	t.Helper()
	first, err := svc.Store.GetReservation(ctx, reservationIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	refs := make([]SubmittedReservationRef, 0, len(reservationIDs))
	if first.OperationID == "" {
		for _, reservationID := range reservationIDs {
			lease, leaseErr := svc.AcquireLease(ctx, reservationID, "reconcile-test-worker", time.Minute)
			if leaseErr != nil {
				t.Fatal(leaseErr)
			}
			if _, transitionErr := svc.TransitionWithLease(ctx, reservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); transitionErr != nil {
				t.Fatal(transitionErr)
			}
			if _, transitionErr := svc.TransitionWithLease(ctx, reservationID, lease.Owner, lease.Token, StatusProving, StatusProofReady); transitionErr != nil {
				t.Fatal(transitionErr)
			}
			refs = append(refs, SubmittedReservationRef{ReservationID: reservationID, LeaseOwner: lease.Owner, LeaseToken: lease.Token})
		}
	} else {
		refs, _, err = svc.BeginProvingOperation(ctx, first.OperationID, reservationIDs, "reconcile-test-worker", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{
			OperationID: first.OperationID,
			PayloadHash: "reconcile-payload",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := svc.MarkBroadcastAttempting(ctx, refs, nil, BroadcastAttemptStart{
		TxHash:      "txhash",
		TxBytesHash: "tx-bytes",
		SignDocHash: "sign-doc",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkSubmittedBatch(ctx, refs, nil, SubmittedReservationUpdate{TxHash: "txhash", TxBytesHash: "tx-bytes", SignDocHash: "sign-doc", AccountSequence: 1}); err != nil {
		t.Fatal(err)
	}
}

type reconcileUpdateFailingStore struct {
	Store
	err error
}

func (s reconcileUpdateFailingStore) ApplyReconciliationTransition(context.Context, ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
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
			_, _, _ = s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
				ReservationID:     reservationID,
				From:              reservation.Status,
				To:                s.status,
				Now:               fixedNow(),
				serviceAuthorized: true,
			})
		})
	}
	return reservation, nil
}
