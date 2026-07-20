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

	_, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "")})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r2", "note-b", "")})
	if !errors.Is(err, ErrActiveReservationExists) {
		t.Fatalf("expected active duplicate rejection, got %v", err)
	}
}

func TestServiceReserveLinksOperationToReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	reservation := testReservation("r1", "note-a", "")
	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: reservation,
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.OperationID != "op-a" {
		t.Fatalf("expected reservation operation_id to be populated, got %+v", created)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.ReservationID != "r1" {
		t.Fatalf("expected operation reservation_id to be populated, got %+v", operation)
	}
}

func TestServiceReserveRejectsMissingOperationLink(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "op-a")})
	if !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("expected missing operation link rejection, got %v", err)
	}
}

type getOperationErrorStore struct {
	Store
	err error
}

func (s getOperationErrorStore) GetOperation(context.Context, string) (*PayrollOperation, error) {
	return nil, s.err
}

func TestServiceReservePreservesOperationStoreFailure(t *testing.T) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	baseService := Service{Store: baseStore, Now: fixedNow}
	if _, err := baseService.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	}); err != nil {
		t.Fatal(err)
	}

	storeFailure := errors.New("database unavailable")
	svc := Service{Store: getOperationErrorStore{Store: baseStore, err: storeFailure}, Now: fixedNow}
	sibling := testReservation("r2", "note-b", "op-a")
	sibling.NullifierLookupKey = "lookup-b"
	_, err := svc.Reserve(ctx, ReserveInput{Reservation: sibling})
	if !errors.Is(err, storeFailure) {
		t.Fatalf("expected Store failure to remain discoverable, got %v", err)
	}
	if errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("expected Store failure not to be reclassified as missing operation: %v", err)
	}
}

func TestServiceReserveRejectsExtendingOperationAfterWorkerActivity(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	sibling := testReservation("r2", "note-b", "op-a")
	sibling.NullifierLookupKey = "lookup-b"
	if _, err := svc.Reserve(ctx, ReserveInput{Reservation: sibling}); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected operation membership extension after worker activity to fail, got %v", err)
	}
	if _, err := store.GetReservation(ctx, sibling.ReservationID); !errors.Is(err, ErrReservationNotFound) {
		t.Fatalf("expected rejected sibling not to be persisted, got %v", err)
	}
}

func TestMemoryStoreAtomicallyRejectsExtendingOperationAfterWorkerActivity(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	sibling := testReservation("r2", "note-b", "op-a")
	sibling.NullifierLookupKey = "lookup-b"
	if _, err := store.CreateReservationBatch(ctx, []NoteReservation{sibling}, nil); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected atomic Store membership guard to reject sibling, got %v", err)
	}
}

func TestServiceReserveRejectsOperationLinkMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-b", Status: OperationStatusPlanned},
	})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected operation_id mismatch rejection, got %v", err)
	}

	_, err = svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:   "op-a",
			ReservationID: "different-reservation",
			Status:        OperationStatusPlanned,
		},
	})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected reservation_id mismatch rejection, got %v", err)
	}
}

func TestServiceTransitionUsesCompareAndSet(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
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

func TestServiceTransitionRejectsActiveDuplicate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "")})
	if err != nil {
		t.Fatal(err)
	}
	replanning := testReservation("r2", "note-b", "op-b")
	replanning.Status = StatusReplanRequired
	if _, err := store.UnsafeImportReservationForTesting(ctx, replanning); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Transition(ctx, "r2", StatusReplanRequired, StatusReserved)
	if !errors.Is(err, ErrActiveReservationExists) {
		t.Fatalf("expected active duplicate rejection, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusReplanRequired {
		t.Fatalf("expected duplicate transition to leave status ReplanRequired, got %s", unchanged.Status)
	}
}

func TestMemoryStoreCreationRejectsForgedLifecycleEvidence(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	for _, reservation := range []NoteReservation{
		func() NoteReservation {
			value := testReservation("submitted", "note-submitted", "")
			value.Status = StatusSubmitted
			value.TxHash = "forged-tx"
			return value
		}(),
		func() NoteReservation {
			value := testReservation("relay", "note-relay", "")
			value.PayloadHash = "forged-payload"
			value.RelayHandedOff = true
			return value
		}(),
	} {
		if _, err := store.CreateReservation(ctx, reservation); !errors.Is(err, ErrInvalidReservation) {
			t.Fatalf("expected forged lifecycle reservation rejection, got %v", err)
		}
	}
	if _, err := store.CreateOperation(ctx, PayrollOperation{
		OperationID: "forged-operation",
		Status:      OperationStatusSubmitted,
		TxHash:      "forged-tx",
	}); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected forged operation rejection, got %v", err)
	}
	if _, err := store.CreateReservation(ctx, testReservation("missing-operation", "note-missing", "op-missing")); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("expected reservation-operation link rejection, got %v", err)
	}
}

func TestServiceReserveNormalizationRejectsForgedLifecycleEvidence(t *testing.T) {
	now := fixedNow()
	reservation := testReservation("forged-reservation", "note-forged", "")
	reservation.PayloadHash = "forged-payload"
	if _, _, err := normalizeReserveInput(ReserveInput{Reservation: reservation}, now); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected Service reservation lifecycle evidence rejection, got %v", err)
	}

	operation := &PayrollOperation{
		OperationID: "forged-operation",
		Status:      OperationStatusPlanned,
		TxHash:      "forged-tx",
	}
	if _, _, err := normalizeReserveInput(ReserveInput{
		Reservation: testReservation("clean-reservation", "note-clean", "forged-operation"),
		Operation:   operation,
	}, now); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected Service operation lifecycle evidence rejection, got %v", err)
	}
}

func TestSubmittedIdentityCannotOverwriteProofReadyIdentity(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, ProofReadyOperationUpdate{
		OperationID: "op-a",
		PayloadHash: "payload-a",
		TxBytesHash: "proof-tx-bytes",
	}); err != nil {
		t.Fatal(err)
	}
	markBroadcastAttemptingForTest(t, ctx, svc, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, []string{"op-a"}, BroadcastAttemptStart{TxBytesHash: "proof-tx-bytes"})
	_, _, err = svc.MarkSubmittedBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, nil, SubmittedReservationUpdate{TxHash: "tx-a", TxBytesHash: "different-tx-bytes"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected conflicting submitted identity rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProofReady || reservation.TxBytesHash != "proof-tx-bytes" {
		t.Fatalf("expected ProofReady identity to remain intact, got %+v", reservation)
	}
}

func TestSubmittedIdentityMatchesProofReadyIdentityCaseInsensitively(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")
	store.reservations[created.ReservationID] = func() NoteReservation {
		reservation := store.reservations[created.ReservationID]
		reservation.TxBytesHash = "0xABCDEF"
		return reservation
	}()
	operation := store.operations["op-a"]
	operation.TxBytesHash = "0xABCDEF"
	store.operations["op-a"] = operation

	markBroadcastAttemptingForTest(t, ctx, svc, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, []string{"op-a"}, BroadcastAttemptStart{TxBytesHash: "abcdef"})
	_, _, err := svc.MarkSubmittedBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, []string{"op-a"}, SubmittedReservationUpdate{TxBytesHash: "abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusSubmitted {
		t.Fatalf("expected Submitted, got %s", reservation.Status)
	}
}

func TestServiceTransitionRejectsLeaseRequiredTransitionWithoutToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Transition(ctx, created.ReservationID, StatusReserved, StatusProving)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected lease mismatch for worker-owned transition, got %v", err)
	}
}

func TestMemoryStoreCompareAndSetRejectsLeaseRequiredTransitionWithoutToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.CompareAndSetReservationStatus(ctx, created.ReservationID, StatusReserved, StatusProving, fixedNow())
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected store CAS to require lease token, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusReserved {
		t.Fatalf("expected reservation to remain Reserved, got %s", unchanged.Status)
	}
}

func TestMemoryStorePublicCASRejectsEvidenceAwareTransition(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, _ := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	_, err := store.CompareAndSetReservationStatus(ctx, created.ReservationID, StatusProofReady, StatusConfirmedSpent, fixedNow())
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected public store CAS to reject evidence-aware transition, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusProofReady {
		t.Fatalf("expected reservation to remain ProofReady, got %s", unchanged.Status)
	}
}

func TestServiceReleaseOnlyReservedAutomatically(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Release(ctx, created.ReservationID, StatusProving)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid release transition, got %v", err)
	}
	_, err = svc.Transition(ctx, created.ReservationID, StatusProving, StatusReleased)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected direct Proving -> Released transition to be rejected, got %v", err)
	}
	markLeasedReservationProofReady(t, ctx, svc, created.ReservationID, lease)
	_, err = svc.Transition(ctx, created.ReservationID, StatusProofReady, StatusReleased)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected direct ProofReady -> Released transition to be rejected, got %v", err)
	}
}

func TestServiceProofReadyLeaseCannotBeReacquiredAfterExpiry(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, first.Owner, first.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	markLeasedReservationProofReady(t, ctx, svc, created.ReservationID, first)

	now = now.Add(2 * time.Minute)
	if _, err := svc.AcquireLeaseForStatus(ctx, created.ReservationID, "worker-b", StatusProofReady, time.Minute); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("expected ProofReady lease reacquisition to fail, got %v", err)
	}

	_, err = svc.MarkSubmitted(ctx, created.ReservationID, first.Owner, first.Token, "stale-tx", "stale-bytes", "stale-sign-doc", 1)
	if !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("expected expired submitted lease rejection, got %v", err)
	}

	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProofReady || reservation.TxHash != "" {
		t.Fatalf("expected ProofReady reservation to remain locked for reconciliation, got %+v", reservation)
	}
}

func TestMemoryStoreMarkReservationSubmittedDerivesLinkedOperation(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	markLeasedReservationProofReady(t, ctx, svc, created.ReservationID, lease)
	markBroadcastAttemptingForTest(t, ctx, svc, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, []string{"op-a"}, BroadcastAttemptStart{
		TxHash:      "txhash",
		TxBytesHash: "tx-bytes",
		SignDocHash: "sign-doc",
	})

	_, err = store.MarkReservationSubmitted(ctx, created.ReservationID, lease.Owner, lease.Token, SubmittedReservationUpdate{
		TxHash:          "txhash",
		TxBytesHash:     "tx-bytes",
		SignDocHash:     "sign-doc",
		AccountSequence: 3,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusSubmitted || operation.TxHash != "txhash" || operation.TxBytesHash != "tx-bytes" || operation.SignDocHash != "sign-doc" {
		t.Fatalf("expected legacy submitted path to update linked operation, got %+v", operation)
	}
}

func TestMemoryStoreMarkSubmittedRejectsMissingTxIdentity(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	_, err := store.MarkReservationSubmitted(ctx, created.ReservationID, lease.Owner, lease.Token, SubmittedReservationUpdate{}, now)
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected missing tx identity rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProofReady {
		t.Fatalf("expected reservation to remain ProofReady, got %s", reservation.Status)
	}
}

func TestMemoryStoreMarkSubmittedRejectsSignDocOnly(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	_, err := store.MarkReservationSubmitted(ctx, created.ReservationID, lease.Owner, lease.Token, SubmittedReservationUpdate{
		SignDocHash: "sign-doc-only",
	}, now)
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected sign-doc-only submitted update rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProofReady {
		t.Fatalf("expected reservation to remain ProofReady, got %s", reservation.Status)
	}
}

func TestMemoryStoreTerminalBookkeepingRequiresDurableBroadcastAttempt(t *testing.T) {
	ctx := context.Background()
	for _, testCase := range []struct {
		name  string
		apply func(Service, *NoteReservation, *Lease) error
	}{
		{
			name: "submitted",
			apply: func(svc Service, created *NoteReservation, lease *Lease) error {
				_, err := svc.MarkSubmitted(ctx, created.ReservationID, lease.Owner, lease.Token, "txhash", "", "", 1)
				return err
			},
		},
		{
			name: "unknown",
			apply: func(svc Service, created *NoteReservation, lease *Lease) error {
				_, _, err := svc.MarkBroadcastUnknownBatch(ctx, []SubmittedReservationRef{{
					ReservationID: created.ReservationID,
					LeaseOwner:    lease.Owner,
					LeaseToken:    lease.Token,
				}}, []string{"op-a"}, BroadcastAttemptUpdate{TxHash: "txhash"})
				return err
			},
		},
		{
			name: "ambiguous",
			apply: func(svc Service, created *NoteReservation, lease *Lease) error {
				_, _, err := svc.MarkBroadcastAmbiguousBatch(ctx, []SubmittedReservationRef{{
					ReservationID: created.ReservationID,
					LeaseOwner:    lease.Owner,
					LeaseToken:    lease.Token,
				}}, []string{"op-a"}, BroadcastAmbiguityUpdate{LastBroadcastError: "response lost"})
				return err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store := NewMemoryStore()
			svc := Service{Store: store, Now: fixedNow}
			created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")
			err := testCase.apply(svc, created, lease)
			if !errors.Is(err, ErrCompareAndSetFailed) {
				t.Fatalf("expected missing durable attempt rejection, got %v", err)
			}
			stored, getErr := store.GetReservation(ctx, created.ReservationID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if stored.Status != StatusProofReady || stored.BroadcastAttemptCount != 0 || stored.BroadcastInFlight {
				t.Fatalf("terminal bookkeeping mutated reservation without an attempt: %+v", stored)
			}
		})
	}
}

func TestMemoryStoreMarkBroadcastUnknownRejectsSignDocOnly(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	_, _, err := svc.MarkBroadcastUnknownBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, []string{"op-a"}, BroadcastAttemptUpdate{SignDocHash: "sign-doc-only"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected sign-doc-only Unknown rejection, got %v", err)
	}
}

func TestServiceRecoversWorkerStateOnlyAfterLeaseExpiry(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	created, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "")})
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

	if _, err := svc.RecoverAfterLeaseExpiry(ctx, created.ReservationID, StatusProving, StatusReplanRequired); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("expected live lease recovery rejection, got %v", err)
	}
	now = now.Add(2 * time.Minute)
	recovered, err := svc.RecoverAfterLeaseExpiry(ctx, created.ReservationID, StatusProving, StatusReplanRequired)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusReplanRequired {
		t.Fatalf("expected ReplanRequired, got %s", recovered.Status)
	}

	proofReady, proofReadyLease := reserveProofReadyReservation(t, ctx, svc, "r2", "note-b", "op-b")
	now = now.Add(2 * time.Minute)
	if _, err := svc.RecoverAfterLeaseExpiry(ctx, proofReady.ReservationID, StatusProofReady, StatusReplanRequired); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected expired ProofReady replan rejection, got %v", err)
	}
	if proofReadyLease.Token == "" {
		t.Fatal("expected ProofReady reservation lease")
	}
	recovered, err = svc.RecoverAfterLeaseExpiry(ctx, proofReady.ReservationID, StatusProofReady, StatusManualReview)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusManualReview {
		t.Fatalf("expected ManualReview, got %s", recovered.Status)
	}
	operation, err := store.GetOperation(ctx, "op-b")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusManualReview {
		t.Fatalf("expected lease-expiry recovery to atomically update operation, got %s", operation.Status)
	}
}

func TestServiceResolveManualReviewRecordsOperatorApprovalAndUpdatesOperation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")
	markBroadcastAttemptingForTest(t, ctx, svc, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, []string{"op-a"}, BroadcastAttemptStart{TxHash: "txhash"})
	if _, err := svc.MarkSubmitted(ctx, created.ReservationID, lease.Owner, lease.Token, "txhash", "", "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reconcile(ctx, created.ReservationID, OperationEvidence{
		TxFailed:                  true,
		TxHash:                    "unexpected-txhash",
		NullifierUnspentConfirmed: true,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ResolveManualReview(ctx, created.ReservationID, ManualReviewResolution{Target: StatusReplanRequired}); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected missing operator approval rejection, got %v", err)
	}
	if _, err := svc.ResolveManualReview(ctx, created.ReservationID, ManualReviewResolution{Target: StatusReplanRequired, OperatorID: "   ", ApprovalReference: "\t"}); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected whitespace-only operator approval rejection, got %v", err)
	}
	resolved, err := svc.ResolveManualReview(ctx, created.ReservationID, ManualReviewResolution{
		Target:            StatusReplanRequired,
		OperatorID:        " ops@example.test ",
		ApprovalReference: " incident-421 ",
		Reason:            "tx absent and nullifier unspent confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != StatusReplanRequired || resolved.ManualReviewResolvedBy != "ops@example.test" || resolved.ManualReviewApprovalReference != "incident-421" {
		t.Fatalf("expected approved ManualReview resolution to be recorded, got %+v", resolved)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusReplanRequired {
		t.Fatalf("expected resolved operation to require replanning, got %s", operation.Status)
	}
}

func TestServiceDoesNotResolveOrRecoverCompletedOperationIntoReusableState(t *testing.T) {
	for _, terminalStatus := range []OperationStatus{
		OperationStatusSucceeded,
		OperationStatusConflictSpent,
	} {
		t.Run(string(terminalStatus), func(t *testing.T) {
			ctx := context.Background()
			now := fixedNow()
			store := NewMemoryStore()
			svc := Service{Store: store, Now: func() time.Time { return now }}
			created, _ := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

			// A completed sibling operation must be reconciled as spent, never
			// downgraded by worker-expiry recovery.
			operation := store.operations["op-a"]
			operation.Status = terminalStatus
			store.operations["op-a"] = operation
			now = now.Add(2 * time.Minute)
			if _, err := svc.RecoverAfterLeaseExpiry(ctx, created.ReservationID, StatusProofReady, StatusManualReview); !errors.Is(err, ErrManualReviewRequired) {
				t.Fatalf("expected terminal operation recovery rejection, got %v", err)
			}
			stored, err := store.GetReservation(ctx, created.ReservationID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != StatusProofReady {
				t.Fatalf("expected completed operation sibling to remain ProofReady for spent reconciliation, got %s", stored.Status)
			}

			// ManualReview resolution must apply the same terminal-operation guard.
			if _, _, err := store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
				ReservationID:     created.ReservationID,
				From:              StatusProofReady,
				To:                StatusManualReview,
				Now:               now,
				serviceAuthorized: true,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ResolveManualReview(ctx, created.ReservationID, ManualReviewResolution{
				Target:            StatusReplanRequired,
				OperatorID:        "ops@example.test",
				ApprovalReference: "incident-422",
			}); !errors.Is(err, ErrManualReviewRequired) {
				t.Fatalf("expected terminal operation resolution rejection, got %v", err)
			}
			updatedOperation, err := store.GetOperation(ctx, "op-a")
			if err != nil {
				t.Fatal(err)
			}
			if updatedOperation.Status != terminalStatus {
				t.Fatalf("expected terminal operation to remain %s, got %s", terminalStatus, updatedOperation.Status)
			}
		})
	}
}

func TestServicePreservesFailedOperationWhenResolvingItsReservation(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	created, _ := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")
	operation := store.operations["op-a"]
	operation.Status = OperationStatusFailed
	store.operations["op-a"] = operation
	now = now.Add(2 * time.Minute)

	if _, err := svc.RecoverAfterLeaseExpiry(ctx, created.ReservationID, StatusProofReady, StatusManualReview); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.ResolveManualReview(ctx, created.ReservationID, ManualReviewResolution{
		Target:            StatusReplanRequired,
		OperatorID:        "ops@example.test",
		ApprovalReference: "incident-423",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != StatusReplanRequired {
		t.Fatalf("expected reservation to be released for a new operation, got %s", resolved.Status)
	}
	updatedOperation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if updatedOperation.Status != OperationStatusFailed {
		t.Fatalf("expected failed operation audit record to remain Failed, got %s", updatedOperation.Status)
	}
}

func TestServiceReplansLiveProofReadyAfterVerifiedLocalProofDiscard(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	replanned, err := svc.ReplanProofReadyAfterDiscard(ctx, created.ReservationID, lease.Owner, lease.Token, ProofDiscardEvidence{
		NoBroadcastAttempt: true,
		ProofDiscarded:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replanned.Status != StatusReplanRequired {
		t.Fatalf("expected ProofReady discard to replan, got %s", replanned.Status)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusReplanRequired {
		t.Fatalf("expected proof discard to update operation, got %s", operation.Status)
	}
}

func TestServiceRecordRelayHandoffPreventsProofDiscard(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	handedOff, err := svc.RecordRelayHandoff(ctx, created.ReservationID, lease.Owner, lease.Token, "payload-op-a")
	if err != nil {
		t.Fatal(err)
	}
	if handedOff.Status != StatusProofReady || !handedOff.RelayHandedOff || handedOff.RelayHandedOffAt.IsZero() {
		t.Fatalf("expected durable relay handoff marker, got %+v", handedOff)
	}
	if _, err := svc.ReplanProofReadyAfterDiscard(ctx, created.ReservationID, lease.Owner, lease.Token, ProofDiscardEvidence{
		NoBroadcastAttempt: true,
		ProofDiscarded:     true,
	}); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected handed-off relay payload to reject proof discard, got %v", err)
	}
	if _, err := svc.RecordRelayHandoff(ctx, created.ReservationID, lease.Owner, "wrong-token", "payload-op-a"); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected relay handoff to require current lease token, got %v", err)
	}
	if _, err := svc.RecordRelayHandoff(ctx, created.ReservationID, lease.Owner, lease.Token, "wrong-payload-hash"); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected relay handoff to require matching payload hash, got %v", err)
	}
	if _, err := svc.ClearLease(ctx, created.ReservationID, lease.Owner, lease.Token); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected handed-off relay payload to reject lease clearing, got %v", err)
	}
	persisted, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.RelayHandedOff || persisted.LeaseToken != lease.Token {
		t.Fatalf("relay handoff lease changed after rejected clear: %+v", persisted)
	}
}

func TestServiceRecordRelayHandoffBatchRequiresCompleteProofReadyOperation(t *testing.T) {
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
	refs := beginProvingOperationForTest(t, ctx, svc, "op-a", "r1", "r2")
	if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-op-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordRelayHandoff(ctx, "r1", refs[0].LeaseOwner, refs[0].LeaseToken, "payload-op-a"); !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected partial single relay handoff rejection, got %v", err)
	}
	updated, err := svc.RecordRelayHandoffBatch(ctx, "op-a", refs, "payload-op-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 2 || !updated[0].RelayHandedOff || !updated[1].RelayHandedOff {
		t.Fatalf("expected all inputs to receive relay handoff marker, got %+v", updated)
	}
}

func TestStoreRejectsEmptyOperationIDForLinkedRelayHandoff(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	_, err := store.RecordRelayHandoffBatch(ctx, "", []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, "payload-op-a", fixedNow())
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected linked reservation to reject empty operation id, got %v", err)
	}
	persisted, getErr := store.GetReservation(ctx, created.ReservationID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if persisted.RelayHandedOff {
		t.Fatalf("linked reservation was partially handed off: %+v", persisted)
	}
}

func TestServiceGenericTransitionRejectsReconcileOnlyStateChange(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")
	markBroadcastAttemptingForTest(t, ctx, svc, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, []string{"op-a"}, BroadcastAttemptStart{TxHash: "txhash"})
	if _, err := svc.MarkSubmitted(ctx, created.ReservationID, lease.Owner, lease.Token, "txhash", "", "", 1); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Transition(ctx, created.ReservationID, StatusSubmitted, StatusFailed)
	if !errors.Is(err, ErrManualReviewRequired) {
		t.Fatalf("expected reconcile evidence requirement, got %v", err)
	}
}

func TestServiceTransitionRejectsBroadcastUnknownWithoutToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	markLeasedReservationProofReady(t, ctx, svc, created.ReservationID, lease)

	_, err = svc.Transition(ctx, created.ReservationID, StatusProofReady, StatusUnknown)
	if !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected lease mismatch for ProofReady -> Unknown, got %v", err)
	}

	markBroadcastAttemptingForTest(t, ctx, svc, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, []string{"op-a"}, BroadcastAttemptStart{
		TxHash:      "ambiguous-tx",
		TxBytesHash: "ambiguous-bytes",
		SignDocHash: "ambiguous-sign-doc",
	})
	updated, operations, err := svc.MarkBroadcastUnknownBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner, LeaseToken: lease.Token,
	}}, nil, BroadcastAttemptUpdate{
		TxHash:             "ambiguous-tx",
		TxBytesHash:        "ambiguous-bytes",
		SignDocHash:        "ambiguous-sign-doc",
		LastBroadcastError: "rpc timeout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].Status != StatusUnknown || updated[0].TxHash != "ambiguous-tx" {
		t.Fatalf("expected leased broadcast-unknown update, got %+v", updated)
	}
	if len(operations) != 1 || operations[0].Status != OperationStatusUnknown || operations[0].TxHash != "ambiguous-tx" {
		t.Fatalf("expected linked operation unknown metadata, got %+v", operations)
	}
}

func TestMemoryStoreAtomicallyRejectsSingleReservationCommandForMultiInputOperation(t *testing.T) {
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

	_, _, err = store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:                     "r1",
		From:                              StatusReserved,
		To:                                StatusManualReview,
		Now:                               fixedNow(),
		serviceAuthorized:                 true,
		requireSingleReservationOperation: true,
	})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected atomic multi-input rejection, got %v", err)
	}
	stored, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusReserved {
		t.Fatalf("expected rejected transition to leave reservation Reserved, got %s", stored.Status)
	}
}

func TestMemoryStoreReconciliationRejectsStaleTerminalOperationOverwrite(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, _ := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	terminal := store.operations["op-a"]
	terminal.Status = OperationStatusConflictSpent
	store.operations["op-a"] = terminal
	_, _, err := store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID: created.ReservationID,
		From:          StatusProofReady,
		To:            StatusConfirmedSpent,
		Operation: &PayrollOperation{
			OperationID: "op-a",
			Status:      OperationStatusSucceeded,
		},
		Now:               fixedNow(),
		serviceAuthorized: true,
	})
	if !errors.Is(err, ErrCompareAndSetFailed) {
		t.Fatalf("expected stale terminal operation rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProofReady {
		t.Fatalf("expected reservation update to roll back with stale operation, got %s", reservation.Status)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusConflictSpent {
		t.Fatalf("expected terminal operation to remain ConflictSpent, got %s", operation.Status)
	}
}

func TestMemoryStoreHighLevelUpdatesDoNotRegressTerminalOperation(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()

	t.Run("proof ready", func(t *testing.T) {
		store := NewMemoryStore()
		svc := Service{Store: store, Now: func() time.Time { return now }}
		created, err := svc.Reserve(ctx, ReserveInput{
			Reservation: testReservation("proof-ready", "note-proof-ready", "op-proof-ready"),
			Operation:   &PayrollOperation{OperationID: "op-proof-ready", Status: OperationStatusPlanned},
		})
		if err != nil {
			t.Fatal(err)
		}
		lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
			t.Fatal(err)
		}
		operation := store.operations["op-proof-ready"]
		operation.Status = OperationStatusSucceeded
		store.operations[operation.OperationID] = operation
		_, _, err = store.MarkReservationsProofReady(ctx, []SubmittedReservationRef{{
			ReservationID: created.ReservationID,
			LeaseOwner:    lease.Owner, LeaseToken: lease.Token,
		}}, ProofReadyOperationUpdate{OperationID: operation.OperationID}, now)
		if !errors.Is(err, ErrCompareAndSetFailed) {
			t.Fatalf("expected terminal operation rejection, got %v", err)
		}
		stored, _ := store.GetReservation(ctx, created.ReservationID)
		if stored.Status != StatusProving {
			t.Fatalf("expected reservation to remain Proving, got %s", stored.Status)
		}
	})

	for _, testCase := range []struct {
		name   string
		update func(*MemoryStore, *NoteReservation, *Lease) error
	}{
		{
			name: "submitted",
			update: func(store *MemoryStore, created *NoteReservation, lease *Lease) error {
				_, _, err := store.MarkReservationsSubmitted(ctx, []SubmittedReservationRef{{
					ReservationID: created.ReservationID,
					LeaseOwner:    lease.Owner, LeaseToken: lease.Token,
				}}, nil, SubmittedReservationUpdate{TxHash: "stale-tx"}, now)
				return err
			},
		},
		{
			name: "unknown",
			update: func(store *MemoryStore, created *NoteReservation, lease *Lease) error {
				_, _, err := store.MarkReservationsBroadcastUnknown(ctx, []SubmittedReservationRef{{
					ReservationID: created.ReservationID,
					LeaseOwner:    lease.Owner, LeaseToken: lease.Token,
				}}, nil, BroadcastAttemptUpdate{TxHash: "stale-tx"}, now)
				return err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store := NewMemoryStore()
			svc := Service{Store: store, Now: func() time.Time { return now }}
			created, lease := reserveProofReadyReservation(t, ctx, svc, "r1-"+testCase.name, "note-"+testCase.name, "op-"+testCase.name)
			operation := store.operations[created.OperationID]
			operation.Status = OperationStatusConflictSpent
			store.operations[operation.OperationID] = operation
			if err := testCase.update(store, created, lease); !errors.Is(err, ErrCompareAndSetFailed) {
				t.Fatalf("expected terminal operation rejection, got %v", err)
			}
			stored, _ := store.GetReservation(ctx, created.ReservationID)
			if stored.Status != StatusProofReady {
				t.Fatalf("expected reservation to remain ProofReady, got %s", stored.Status)
			}
			updatedOperation, _ := store.GetOperation(ctx, created.OperationID)
			if updatedOperation.Status != OperationStatusConflictSpent {
				t.Fatalf("expected terminal operation to stay ConflictSpent, got %s", updatedOperation.Status)
			}
		})
	}
}

func TestMemoryStoreRejectsDirectEvidenceSensitiveTransitionWithLease(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "direct-evidence", "note-direct-evidence", "op-direct-evidence")

	_, err := store.CompareAndSetReservationStatusWithLease(
		ctx,
		created.ReservationID,
		lease.Owner, lease.Token,
		StatusProofReady,
		StatusSubmitted,
		now,
	)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected direct evidence-sensitive transition rejection, got %v", err)
	}
}

func TestServiceMarkBroadcastUnknownRejectsMissingTxIdentity(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	created, lease := reserveProofReadyReservation(t, ctx, svc, "r1", "note-a", "op-a")

	_, _, err := svc.MarkBroadcastUnknownBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner, LeaseToken: lease.Token,
	}}, nil, BroadcastAttemptUpdate{LastBroadcastError: "rpc timeout"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected missing tx identity rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProofReady {
		t.Fatalf("expected reservation to remain ProofReady, got %s", reservation.Status)
	}
}

func TestServiceNilStoreMethodsReturnErrors(t *testing.T) {
	ctx := context.Background()
	svc := Service{}

	if _, err := svc.MarkSubmitted(ctx, "r1", "worker-a", "lease-token", "tx", "tx-bytes", "sign-doc", 1); err == nil {
		t.Fatalf("expected MarkSubmitted to reject nil store")
	}
	if _, err := svc.HeartbeatLease(ctx, "r1", "worker-a", "lease-token", time.Minute); err == nil {
		t.Fatalf("expected HeartbeatLease to reject nil store")
	}
	if _, err := svc.ClearLease(ctx, "r1", "worker-a", "lease-token"); err == nil {
		t.Fatalf("expected ClearLease to reject nil store")
	}
}

func TestServiceBeginAndRollbackProvingOperationAreAtomic(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	second := testReservation("r2", "note-b", "op-a")
	second.NullifierLookupKey = "lookup-b"
	if _, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{Reservation: second},
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.BeginProvingOperation(ctx, "op-a", []string{"r1", "missing"}, "proof-worker", time.Minute); !errors.Is(err, ErrReservationNotFound) {
		t.Fatalf("expected atomic claim preflight failure, got %v", err)
	}
	for _, reservationID := range []string{"r1", "r2"} {
		reservation, err := store.GetReservation(ctx, reservationID)
		if err != nil {
			t.Fatal(err)
		}
		if reservation.Status != StatusReserved || reservation.LeaseToken != "" {
			t.Fatalf("failed claim mutated reservation %s: %+v", reservationID, reservation)
		}
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusPlanned {
		t.Fatalf("failed claim mutated operation: %+v", operation)
	}

	refs, operation, err := svc.BeginProvingOperation(ctx, "op-a", []string{"r1", "r2"}, " proof-worker ", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || operation.Status != OperationStatusProving {
		t.Fatalf("unexpected proving claim result: refs=%+v operation=%+v", refs, operation)
	}
	for _, ref := range refs {
		reservation, err := store.GetReservation(ctx, ref.ReservationID)
		if err != nil {
			t.Fatal(err)
		}
		if reservation.Status != StatusProving || reservation.LeaseOwner != "proof-worker" || reservation.LeaseToken == "" {
			t.Fatalf("reservation was not atomically claimed: %+v", reservation)
		}
	}

	rolledBack, operation, err := svc.RollbackProvingOperation(ctx, "op-a", refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(rolledBack) != 2 || operation.Status != OperationStatusPlanned {
		t.Fatalf("unexpected rollback result: reservations=%+v operation=%+v", rolledBack, operation)
	}
	for _, reservation := range rolledBack {
		if reservation.Status != StatusReserved || reservation.LeaseOwner != "" || reservation.LeaseToken != "" || !reservation.LeaseUntil.IsZero() {
			t.Fatalf("rollback did not restore clean Reserved state: %+v", reservation)
		}
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

	refs := beginProvingOperationForTest(t, ctx, svc, "op-a", "r1", "r2")

	_, _, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{
		refs[0],
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
	if operation.Status != OperationStatusProving || operation.ExpectedOutputCommitment != "" {
		t.Fatalf("expected operation to remain proving, got %+v", operation)
	}

	_, operation, err = svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{
		OperationID:              "op-a",
		PayloadHash:              "payload-a",
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

func TestBeginProvingOperationRejectsMissingInactiveLinkedReservation(t *testing.T) {
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
	if _, err := svc.Transition(ctx, "r2", StatusReserved, StatusReplanRequired); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.BeginProvingOperation(ctx, "op-a", []string{"r1"}, "worker-a", time.Minute)
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected inactive linked reservation to keep the exact-set guard, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusReserved || unchanged.LeaseToken != "" {
		t.Fatalf("expected r1 to remain unclaimed, got %+v", unchanged)
	}
}

func TestLinkedProofReadyRequiresPayloadBoundBatchCommand(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusProving, StatusProofReady); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected service transition to require MarkProofReadyBatch, got %v", err)
	}
	if _, err := store.CompareAndSetReservationStatusWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusProving, StatusProofReady, fixedNow()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected store CAS to require a payload-bound command, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusProving || unchanged.PayloadHash != "" {
		t.Fatalf("generic CAS changed payload-bound state: %+v", unchanged)
	}
}

func TestServiceMarkProofReadyBatchRejectsLinkedOperationWithoutOperationID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLeaseForStatus(ctx, created.ReservationID, "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{
		{ReservationID: created.ReservationID, LeaseOwner: lease.Owner, LeaseToken: lease.Token},
	}, ProofReadyOperationUpdate{ExpectedOutputCommitment: "commitment-a"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected linked proof-ready update without operation_id to be rejected, got %v", err)
	}
	unchanged, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != StatusProving {
		t.Fatalf("expected reservation to remain Proving, got %s", unchanged.Status)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusPlanned || operation.ExpectedOutputCommitment != "" {
		t.Fatalf("expected operation to remain planned without evidence, got %+v", operation)
	}
}

func TestServiceMarkProofReadyBatchRejectsExpectedEvidenceMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}

	_, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation: &PayrollOperation{
			OperationID:              "op-a",
			ExpectedOutputCommitment: "planned-commitment",
			ExpectedDisclosureDigest: "planned-digest",
			Status:                   OperationStatusPlanned,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLeaseForStatus(ctx, "r1", "proof-worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r1", lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseOwner: lease.Owner, LeaseToken: lease.Token},
	}, ProofReadyOperationUpdate{
		OperationID:              "op-a",
		ExpectedOutputCommitment: "actual-commitment",
		ExpectedDisclosureDigest: "planned-digest",
	})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected expected evidence mismatch rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusProving {
		t.Fatalf("expected reservation to remain Proving, got %s", reservation.Status)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusPlanned || operation.ExpectedOutputCommitment != "planned-commitment" || operation.ExpectedDisclosureDigest != "planned-digest" {
		t.Fatalf("expected operation to remain planned with original evidence, got %+v", operation)
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

func TestServiceMarkProofReadyBatchRejectsCrossOperationReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{
			Reservation: func() NoteReservation {
				reservation := testReservation("r2", "note-b", "op-b")
				reservation.NullifierLookupKey = "lookup-b"
				return reservation
			}(),
			Operation: &PayrollOperation{OperationID: "op-b", Status: OperationStatusPlanned},
		},
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
	if _, err := svc.TransitionWithLease(ctx, "r1", firstLease.Owner, firstLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, "r2", secondLease.Owner, secondLease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseOwner: firstLease.Owner, LeaseToken: firstLease.Token},
		{ReservationID: "r2", LeaseOwner: secondLease.Owner, LeaseToken: secondLease.Token},
	}, ProofReadyOperationUpdate{OperationID: "op-a"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected cross-operation proof-ready batch to be rejected, got %v", err)
	}
}

func TestServiceRejectsSingleProvingTransitionForMultiInputOperation(t *testing.T) {
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
	_, err = svc.AcquireLeaseForStatus(ctx, "r1", "proof-worker-a", StatusReserved, time.Minute)
	if !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("expected multi-input single lease rejection, got %v", err)
	}
	reservation, err := store.GetReservation(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != StatusReserved {
		t.Fatalf("expected reservation to remain Reserved, got %s", reservation.Status)
	}
}

func TestServiceMarkSubmittedBatchRejectsCrossOperationIDs(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{
			Reservation: func() NoteReservation {
				reservation := testReservation("r2", "note-b", "op-b")
				reservation.NullifierLookupKey = "lookup-b"
				return reservation
			}(),
			Operation: &PayrollOperation{OperationID: "op-b", Status: OperationStatusPlanned},
		},
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
	for _, ref := range []SubmittedReservationRef{
		{ReservationID: "r1", LeaseOwner: firstLease.Owner, LeaseToken: firstLease.Token},
		{ReservationID: "r2", LeaseOwner: secondLease.Owner, LeaseToken: secondLease.Token},
	} {
		if _, err := svc.TransitionWithLease(ctx, ref.ReservationID, ref.LeaseOwner, ref.LeaseToken, StatusReserved, StatusProving); err != nil {
			t.Fatal(err)
		}
		markLeasedReservationProofReady(t, ctx, svc, ref.ReservationID, &Lease{
			Owner: ref.LeaseOwner,
			Token: ref.LeaseToken,
		})
	}
	markBroadcastAttemptingForTest(t, ctx, svc, []SubmittedReservationRef{{
		ReservationID: "r1",
		LeaseOwner:    firstLease.Owner,
		LeaseToken:    firstLease.Token,
	}}, []string{"op-a"}, BroadcastAttemptStart{TxHash: "tx"})

	_, _, err = svc.MarkSubmittedBatch(ctx, []SubmittedReservationRef{
		{ReservationID: "r1", LeaseOwner: firstLease.Owner, LeaseToken: firstLease.Token},
	}, []string{"op-b"}, SubmittedReservationUpdate{TxHash: "tx"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected cross-operation submitted batch to be rejected, got %v", err)
	}
}

func TestServiceMarkSubmittedBatchRejectsMissingOperationReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{
			Reservation: func() NoteReservation {
				reservation := testReservation("r2", "note-b", "op-a")
				reservation.NullifierLookupKey = "lookup-b"
				return reservation
			}(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := beginProvingOperationForTest(t, ctx, svc, "op-a", "r1", "r2")
	if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-op-a"}); err != nil {
		t.Fatal(err)
	}
	markBroadcastAttemptingForTest(t, ctx, svc, refs, []string{"op-a"}, BroadcastAttemptStart{TxHash: "tx"})

	_, _, err = svc.MarkSubmittedBatch(ctx, []SubmittedReservationRef{
		refs[0],
	}, []string{"op-a"}, SubmittedReservationUpdate{TxHash: "tx"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected missing sibling reservation to be rejected, got %v", err)
	}
}

func TestServiceMarkBroadcastUnknownBatchRejectsMissingOperationReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	_, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{
			Reservation: func() NoteReservation {
				reservation := testReservation("r2", "note-b", "op-a")
				reservation.NullifierLookupKey = "lookup-b"
				return reservation
			}(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := beginProvingOperationForTest(t, ctx, svc, "op-a", "r1", "r2")
	if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-op-a"}); err != nil {
		t.Fatal(err)
	}
	markBroadcastAttemptingForTest(t, ctx, svc, refs, []string{"op-a"}, BroadcastAttemptStart{TxHash: "tx"})

	_, _, err = svc.MarkBroadcastUnknownBatch(ctx, []SubmittedReservationRef{
		refs[0],
	}, []string{"op-a"}, BroadcastAttemptUpdate{TxHash: "tx", LastBroadcastError: "rpc timeout"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected missing sibling reservation to be rejected, got %v", err)
	}
}

func TestServiceRejectsSingleReservationRecoveryForMultiInputOperation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	second := testReservation("r2", "note-b", "op-a")
	second.NullifierLookupKey = "lookup-b"
	if _, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{Reservation: second},
	}); err != nil {
		t.Fatal(err)
	}
	refs := beginProvingOperationForTest(t, ctx, svc, "op-a", "r1", "r2")
	if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-a"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ReplanProofReadyAfterDiscard(ctx, "r1", refs[0].LeaseOwner, refs[0].LeaseToken, ProofDiscardEvidence{
		NoBroadcastAttempt: true,
		ProofDiscarded:     true,
	})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected atomic recovery command rejection, got %v", err)
	}
	for _, reservationID := range []string{"r1", "r2"} {
		reservation, getErr := store.GetReservation(ctx, reservationID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if reservation.Status != StatusProofReady {
			t.Fatalf("expected %s to remain ProofReady, got %s", reservationID, reservation.Status)
		}
	}
}

func TestServiceRecoversMultiInputOperationAtomically(t *testing.T) {
	ctx := context.Background()
	newOperation := func(t *testing.T, now *time.Time) (*MemoryStore, Service, []SubmittedReservationRef) {
		t.Helper()
		store := NewMemoryStore()
		svc := Service{Store: store, Now: func() time.Time { return *now }}
		second := testReservation("r2", "note-b", "op-a")
		second.NullifierLookupKey = "lookup-b"
		if _, err := svc.ReserveBatch(ctx, []ReserveInput{
			{
				Reservation: testReservation("r1", "note-a", "op-a"),
				Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
			},
			{Reservation: second},
		}); err != nil {
			t.Fatal(err)
		}
		return store, svc, beginProvingOperationForTest(t, ctx, svc, "op-a", "r1", "r2")
	}

	t.Run("expired proving leases", func(t *testing.T) {
		now := fixedNow()
		store, svc, _ := newOperation(t, &now)
		now = now.Add(2 * time.Minute)
		if _, err := svc.RecoverOperationAfterLeaseExpiry(ctx, "op-a", []string{"r1"}, StatusProving, StatusReplanRequired); !errors.Is(err, ErrInvalidReservation) {
			t.Fatalf("expected partial operation recovery rejection, got %v", err)
		}
		for _, reservationID := range []string{"r1", "r2"} {
			reservation, _ := store.GetReservation(ctx, reservationID)
			if reservation.Status != StatusProving {
				t.Fatalf("partial preflight mutated %s: %s", reservationID, reservation.Status)
			}
		}
		svc.Store = &postTransitionReadFailingStore{Store: store}
		updated, err := svc.RecoverOperationAfterLeaseExpiry(ctx, "op-a", []string{"r1", "r2"}, StatusProving, StatusReplanRequired)
		if err != nil {
			t.Fatal(err)
		}
		if len(updated) != 2 || updated[0].Status != StatusReplanRequired || updated[1].Status != StatusReplanRequired {
			t.Fatalf("expected atomic operation recovery, got %+v", updated)
		}
		operation, _ := store.GetOperation(ctx, "op-a")
		if operation.Status != OperationStatusReplanRequired {
			t.Fatalf("expected ReplanRequired operation, got %s", operation.Status)
		}
	})

	t.Run("proof discard", func(t *testing.T) {
		now := fixedNow()
		store, svc, refs := newOperation(t, &now)
		if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-a"}); err != nil {
			t.Fatal(err)
		}
		svc.Store = &postTransitionReadFailingStore{Store: store}
		updated, err := svc.ReplanProofReadyOperationAfterDiscard(ctx, "op-a", refs, ProofDiscardEvidence{
			NoBroadcastAttempt: true,
			ProofDiscarded:     true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(updated) != 2 || updated[0].Status != StatusReplanRequired || updated[1].Status != StatusReplanRequired {
			t.Fatalf("expected atomic proof discard, got %+v", updated)
		}
		operation, _ := store.GetOperation(ctx, "op-a")
		if operation.Status != OperationStatusReplanRequired {
			t.Fatalf("expected ReplanRequired operation, got %s", operation.Status)
		}
	})

	t.Run("manual review resolution", func(t *testing.T) {
		now := fixedNow()
		store, svc, refs := newOperation(t, &now)
		if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-a"}); err != nil {
			t.Fatal(err)
		}
		markBroadcastAttemptingForTest(t, ctx, svc, refs, []string{"op-a"}, BroadcastAttemptStart{})
		if _, _, err := svc.MarkBroadcastAmbiguousBatch(ctx, refs, []string{"op-a"}, BroadcastAmbiguityUpdate{LastBroadcastError: "rpc response lost"}); err != nil {
			t.Fatal(err)
		}
		svc.Store = &postTransitionReadFailingStore{Store: store}
		updated, err := svc.ResolveManualReviewOperation(ctx, "op-a", []string{"r1", "r2"}, ManualReviewResolution{
			Target:            StatusReplanRequired,
			OperatorID:        "ops@example.test",
			ApprovalReference: "incident-500",
			Reason:            "tx absent and nullifiers unspent",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, reservation := range updated {
			if reservation.Status != StatusReplanRequired || reservation.ManualReviewResolvedBy != "ops@example.test" {
				t.Fatalf("unexpected resolved reservation: %+v", reservation)
			}
		}
		operation, _ := store.GetOperation(ctx, "op-a")
		if operation.Status != OperationStatusReplanRequired {
			t.Fatalf("expected ReplanRequired operation, got %s", operation.Status)
		}
	})

	t.Run("manual review preserves failed operation", func(t *testing.T) {
		now := fixedNow()
		store, svc, refs := newOperation(t, &now)
		if _, _, err := svc.MarkProofReadyBatch(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-a"}); err != nil {
			t.Fatal(err)
		}
		markBroadcastAttemptingForTest(t, ctx, svc, refs, []string{"op-a"}, BroadcastAttemptStart{})
		if _, _, err := svc.MarkBroadcastAmbiguousBatch(ctx, refs, []string{"op-a"}, BroadcastAmbiguityUpdate{LastBroadcastError: "rpc response lost"}); err != nil {
			t.Fatal(err)
		}
		operation := store.operations["op-a"]
		operation.Status = OperationStatusFailed
		store.operations["op-a"] = operation

		updated, err := svc.ResolveManualReviewOperation(ctx, "op-a", []string{"r1", "r2"}, ManualReviewResolution{
			Target:            StatusReplanRequired,
			OperatorID:        "ops@example.test",
			ApprovalReference: "incident-501",
			Reason:            "failed tx and unspent nullifiers confirmed",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, reservation := range updated {
			if reservation.Status != StatusReplanRequired {
				t.Fatalf("unexpected resolved reservation: %+v", reservation)
			}
		}
		persistedOperation, _ := store.GetOperation(ctx, "op-a")
		if persistedOperation.Status != OperationStatusFailed {
			t.Fatalf("expected terminal Failed operation to be preserved, got %s", persistedOperation.Status)
		}
	})
}

type postTransitionReadFailingStore struct {
	Store
	failReservationReads bool
}

func (s *postTransitionReadFailingStore) GetReservation(ctx context.Context, reservationID string) (*NoteReservation, error) {
	if s.failReservationReads {
		return nil, errors.New("post-transition reservation read failed")
	}
	return s.Store.GetReservation(ctx, reservationID)
}

func (s *postTransitionReadFailingStore) ApplyReconciliationTransition(ctx context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	reservation, operation, err := s.Store.ApplyReconciliationTransition(ctx, transition)
	if err == nil {
		s.failReservationReads = true
	}
	return reservation, operation, err
}

func (s *postTransitionReadFailingStore) ApplyLeaseExpiryRecovery(ctx context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	reservation, operation, err := s.Store.ApplyLeaseExpiryRecovery(ctx, transition)
	if err == nil {
		s.failReservationReads = true
	}
	return reservation, operation, err
}

func (s *postTransitionReadFailingStore) ApplyProofDiscardTransition(ctx context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	reservation, operation, err := s.Store.ApplyProofDiscardTransition(ctx, transition)
	if err == nil {
		s.failReservationReads = true
	}
	return reservation, operation, err
}

func TestMemoryStoreMarkProofReadyRejectsEmptyPayloadHash(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, ProofReadyOperationUpdate{OperationID: "op-a"})
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected empty payload hash rejection, got %v", err)
	}
	reservation, _ := store.GetReservation(ctx, created.ReservationID)
	if reservation.Status != StatusProving {
		t.Fatalf("expected reservation to remain Proving, got %s", reservation.Status)
	}
}

func TestMemoryStoreMarkReservationsProofReadyPreflightsAllReservations(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	second := testReservation("r2", "note-b", "op-a")
	second.NullifierLookupKey = "lookup-b"
	if _, err := svc.ReserveBatch(ctx, []ReserveInput{
		{
			Reservation: testReservation("r1", "note-a", "op-a"),
			Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
		},
		{Reservation: second},
	}); err != nil {
		t.Fatal(err)
	}
	refs := beginProvingOperationForTest(t, ctx, svc, "op-a", "r1", "r2")
	storedSecond := store.reservations["r2"]
	storedSecond.PayloadHash = "payload-other"
	store.reservations["r2"] = storedSecond

	_, _, err := store.MarkReservationsProofReady(ctx, refs, ProofReadyOperationUpdate{OperationID: "op-a", PayloadHash: "payload-a"}, now)
	if !errors.Is(err, ErrInvalidReservation) {
		t.Fatalf("expected payload mismatch rejection, got %v", err)
	}
	for _, reservationID := range []string{"r1", "r2"} {
		reservation, getErr := store.GetReservation(ctx, reservationID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if reservation.Status != StatusProving {
			t.Fatalf("expected %s to remain Proving, got %s", reservationID, reservation.Status)
		}
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != OperationStatusProving || operation.PayloadHash != "" {
		t.Fatalf("expected operation to remain unchanged, got %+v", operation)
	}
}

func TestMemoryStoreCannotRewriteProofReadySuccessPredicate(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: func() time.Time { return now }}
	if _, err := svc.ReserveBatch(ctx, []ReserveInput{{
		Reservation: testReservation("r1", "note-a", "op-a"),
		Operation:   &PayrollOperation{OperationID: "op-a", Status: OperationStatusPlanned},
	}}); err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLeaseForStatus(ctx, "r1", "proof-worker", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ref := SubmittedReservationRef{ReservationID: "r1", LeaseOwner: lease.Owner, LeaseToken: lease.Token}
	if _, err := svc.TransitionWithLease(ctx, "r1", lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	original := ProofReadyOperationUpdate{
		OperationID:              "op-a",
		PayloadHash:              "payload-a",
		ExpectedOutputCommitment: "output-a",
		ExpectedDisclosureDigest: "disclosure-a",
	}
	if _, _, err := store.MarkReservationsProofReady(ctx, []SubmittedReservationRef{ref}, original, now); err != nil {
		t.Fatal(err)
	}
	forged := original
	forged.ExpectedOutputCommitment = "output-forged"
	if _, _, err := store.MarkReservationsProofReady(ctx, []SubmittedReservationRef{ref}, forged, now.Add(time.Second)); !errors.Is(err, ErrCompareAndSetFailed) {
		t.Fatalf("expected ProofReady predicate rewrite to fail status CAS, got %v", err)
	}
	operation, err := store.GetOperation(ctx, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	if operation.ExpectedOutputCommitment != original.ExpectedOutputCommitment {
		t.Fatalf("expected output commitment was rewritten: %q", operation.ExpectedOutputCommitment)
	}
}

func TestServiceReusableTransitionsClearStaleLease(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Now: fixedNow}
	created, err := svc.Reserve(ctx, ReserveInput{Reservation: testReservation("r1", "note-a", "")})
	if err != nil {
		t.Fatal(err)
	}
	firstLease, err := svc.AcquireLeaseForStatus(ctx, created.ReservationID, "worker-a", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, created.ReservationID, StatusReserved, StatusReleased); err != nil {
		t.Fatal(err)
	}
	released, err := store.GetReservation(ctx, created.ReservationID)
	if err != nil {
		t.Fatal(err)
	}
	if released.LeaseToken != "" || !released.LeaseUntil.IsZero() {
		t.Fatalf("expected release transition to clear lease, got %+v", released)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, firstLease.Owner, firstLease.Token, StatusReleased, StatusAvailable); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("expected stale token to be unusable, got %v", err)
	}
	if _, err := svc.Transition(ctx, created.ReservationID, StatusReleased, StatusAvailable); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, created.ReservationID, StatusAvailable, StatusReserved); err != nil {
		t.Fatal(err)
	}
	secondLease, err := svc.AcquireLeaseForStatus(ctx, created.ReservationID, "worker-b", StatusReserved, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if secondLease.Token == firstLease.Token {
		t.Fatal("expected a fresh lease token after reuse")
	}
}

func beginProvingOperationForTest(t *testing.T, ctx context.Context, svc Service, operationID string, reservationIDs ...string) []SubmittedReservationRef {
	t.Helper()
	refs, _, err := svc.BeginProvingOperation(ctx, operationID, reservationIDs, "proof-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

func markBroadcastAttemptingForTest(t *testing.T, ctx context.Context, svc Service, refs []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptStart) {
	t.Helper()
	if _, _, err := svc.MarkBroadcastAttempting(ctx, refs, operationIDs, update); err != nil {
		t.Fatal(err)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC)
}

func markLeasedReservationProofReady(t *testing.T, ctx context.Context, svc Service, reservationID string, lease *Lease) {
	t.Helper()
	reservation, err := svc.Store.GetReservation(ctx, reservationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{{
		ReservationID: reservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, ProofReadyOperationUpdate{
		OperationID: reservation.OperationID,
		PayloadHash: "payload-" + reservationID,
	}); err != nil {
		t.Fatal(err)
	}
}

func reserveProofReadyReservation(t *testing.T, ctx context.Context, svc Service, reservationID string, noteID string, operationID string) (*NoteReservation, *Lease) {
	t.Helper()

	created, err := svc.Reserve(ctx, ReserveInput{
		Reservation: testReservation(reservationID, noteID, operationID),
		Operation:   &PayrollOperation{OperationID: operationID, Status: OperationStatusPlanned},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, StatusReserved, StatusProving); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MarkProofReadyBatch(ctx, []SubmittedReservationRef{{
		ReservationID: created.ReservationID,
		LeaseOwner:    lease.Owner,
		LeaseToken:    lease.Token,
	}}, ProofReadyOperationUpdate{OperationID: operationID, PayloadHash: "payload-" + operationID}); err != nil {
		t.Fatal(err)
	}
	return created, lease
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

func TestNormalizeTransactionIdentityAcceptsUppercaseHexPrefix(t *testing.T) {
	if got := normalizeTransactionIdentity("  0XABCDEF  "); got != "abcdef" {
		t.Fatalf("unexpected normalized identity %q", got)
	}
}
