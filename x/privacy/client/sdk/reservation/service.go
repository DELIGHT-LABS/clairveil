package reservation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	Store Store
	Now   func() time.Time
}

type ReserveInput struct {
	Reservation NoteReservation
	Operation   *PayrollOperation
}

func (s Service) Reserve(ctx context.Context, input ReserveInput) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	created, err := s.ReserveBatch(ctx, []ReserveInput{input})
	if err != nil {
		return nil, err
	}
	return &created[0], nil
}

func (s Service) ReserveBatch(ctx context.Context, inputs []ReserveInput) ([]NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if len(inputs) == 0 {
		return []NoteReservation{}, nil
	}
	now := s.now()

	reservations := make([]NoteReservation, 0, len(inputs))
	operations := make([]PayrollOperation, 0, len(inputs))
	for _, input := range inputs {
		reservation, operation, err := normalizeReserveInput(input, now)
		if err != nil {
			return nil, err
		}
		reservations = append(reservations, reservation)
		if operation != nil {
			operations = append(operations, *operation)
		}
	}
	if err := s.validateReservationOperationLinks(ctx, reservations, operations); err != nil {
		return nil, err
	}

	return s.Store.CreateReservationBatch(ctx, reservations, operations)
}

func (s Service) validateReservationOperationLinks(ctx context.Context, reservations []NoteReservation, operations []PayrollOperation) error {
	pendingOperations := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if operation.OperationID != "" {
			pendingOperations[operation.OperationID] = struct{}{}
		}
	}
	checkedOperations := make(map[string]struct{})
	var storedReservations []NoteReservation
	storedReservationsLoaded := false
	for _, reservation := range reservations {
		if reservation.OperationID == "" {
			continue
		}
		if _, ok := pendingOperations[reservation.OperationID]; ok {
			continue
		}
		if _, ok := checkedOperations[reservation.OperationID]; ok {
			continue
		}
		checkedOperations[reservation.OperationID] = struct{}{}
		operation, err := s.Store.GetOperation(ctx, reservation.OperationID)
		if err != nil {
			if errors.Is(err, ErrOperationNotFound) {
				return fmt.Errorf("%w: reservation %s references missing operation %s", ErrOperationNotFound, reservation.ReservationID, reservation.OperationID)
			}
			return fmt.Errorf("load operation %s for reservation %s: %w", reservation.OperationID, reservation.ReservationID, err)
		}
		if operation == nil {
			return fmt.Errorf("%w: reservation %s references missing operation %s", ErrOperationNotFound, reservation.ReservationID, reservation.OperationID)
		}
		if !storedReservationsLoaded {
			storedReservations, err = s.Store.ListReservations(ctx, ReservationFilter{})
			if err != nil {
				return err
			}
			storedReservationsLoaded = true
		}
		if err := validateOperationMembershipExtension(*operation, storedReservations); err != nil {
			return err
		}
	}
	return nil
}

func validateOperationMembershipExtension(operation PayrollOperation, reservations []NoteReservation) error {
	if err := validateInitialOperationState(operation); err != nil {
		return fmt.Errorf("%w: operation %s membership is closed after lifecycle activity", ErrInvalidReservation, operation.OperationID)
	}
	for _, reservation := range reservations {
		if reservation.OperationID != operation.OperationID {
			continue
		}
		if err := validateInitialReservationState(reservation); err != nil {
			return fmt.Errorf("%w: operation %s membership is closed after reservation %s lifecycle activity", ErrInvalidReservation, operation.OperationID, reservation.ReservationID)
		}
	}
	return nil
}

func normalizeReserveInput(input ReserveInput, now time.Time) (NoteReservation, *PayrollOperation, error) {
	reservation := input.Reservation
	if reservation.ReservationID == "" {
		return NoteReservation{}, nil, fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
	}
	if reservation.OwnerKeyID == "" {
		return NoteReservation{}, nil, fmt.Errorf("%w: owner_key_id is required", ErrInvalidReservation)
	}
	if reservation.NullifierLookupKey == "" {
		return NoteReservation{}, nil, fmt.Errorf("%w: nullifier_lookup_key is required", ErrInvalidReservation)
	}
	if reservation.Status == "" {
		reservation.Status = StatusReserved
	}
	if reservation.Status != StatusReserved {
		return NoteReservation{}, nil, fmt.Errorf("%w: reservation must start as Reserved", ErrInvalidReservation)
	}
	if reservation.CreatedAt.IsZero() {
		reservation.CreatedAt = now
	}
	reservation.UpdatedAt = now
	if err := validateInitialReservationState(reservation); err != nil {
		return NoteReservation{}, nil, err
	}

	var normalizedOperation *PayrollOperation
	if input.Operation != nil {
		operation := *input.Operation
		if operation.OperationID == "" {
			return NoteReservation{}, nil, fmt.Errorf("%w: operation_id is required", ErrInvalidReservation)
		}
		if reservation.OperationID == "" {
			reservation.OperationID = operation.OperationID
		}
		if reservation.OperationID != operation.OperationID {
			return NoteReservation{}, nil, fmt.Errorf("%w: reservation operation_id %s does not match operation %s", ErrInvalidReservation, reservation.OperationID, operation.OperationID)
		}
		if operation.ReservationID == "" {
			operation.ReservationID = reservation.ReservationID
		}
		if operation.ReservationID != reservation.ReservationID {
			return NoteReservation{}, nil, fmt.Errorf("%w: operation reservation_id %s does not match reservation %s", ErrInvalidReservation, operation.ReservationID, reservation.ReservationID)
		}
		if operation.Status == "" {
			operation.Status = OperationStatusPlanned
		}
		if err := validateInitialOperationState(operation); err != nil {
			return NoteReservation{}, nil, err
		}
		if operation.CreatedAt.IsZero() {
			operation.CreatedAt = now
		}
		operation.UpdatedAt = now
		normalizedOperation = &operation
	}
	return reservation, normalizedOperation, nil
}

func (s Service) Transition(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if !CanTransitionReservation(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	if RequiresLeaseToken(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s requires lease token", ErrLeaseMismatch, from, to)
	}
	if RequiresReconcileEvidence(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s requires reconcile evidence", ErrManualReviewRequired, from, to)
	}
	return s.Store.CompareAndSetReservationStatus(ctx, reservationID, from, to, s.now())
}

func (s Service) TransitionWithLease(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, from ReservationStatus, to ReservationStatus) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if !CanTransitionReservation(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	if RequiresReconcileEvidence(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s requires reconcile evidence", ErrManualReviewRequired, from, to)
	}
	if from == StatusProving && to == StatusProofReady {
		reservation, err := s.Store.GetReservation(ctx, reservationID)
		if err != nil {
			return nil, err
		}
		if reservation.OperationID != "" {
			return nil, fmt.Errorf("%w: Proving -> ProofReady requires MarkProofReadyBatch with payload evidence", ErrInvalidTransition)
		}
	}
	if err := s.rejectSingleMultiInputWorkerTransition(ctx, reservationID, from, to); err != nil {
		return nil, err
	}
	return s.Store.CompareAndSetReservationStatusWithLease(ctx, reservationID, leaseOwner, leaseToken, from, to, s.now())
}

func (s Service) rejectSingleMultiInputWorkerTransition(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus) error {
	if !isOperationBatchWorkerTransition(from, to) {
		return nil
	}
	reservation, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return err
	}
	if reservation.OperationID == "" {
		return nil
	}
	reservations, err := s.Store.ListReservations(ctx, ReservationFilter{})
	if err != nil {
		return err
	}
	linked := 0
	for _, candidate := range reservations {
		if candidate.OperationID == reservation.OperationID {
			linked++
			if linked > 1 {
				return fmt.Errorf("%w: multi-input operation %s requires an atomic operation command", ErrInvalidTransition, reservation.OperationID)
			}
		}
	}
	return nil
}

func isOperationBatchWorkerTransition(from ReservationStatus, to ReservationStatus) bool {
	return (from == StatusReserved && to == StatusProving) ||
		(from == StatusProving && (to == StatusReserved || to == StatusProofReady))
}

func (s Service) RecoverAfterLeaseExpiry(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if !CanRecoverAfterLeaseExpiry(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s is not an expired-lease recovery transition", ErrInvalidTransition, from, to)
	}
	reservation, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	if err := s.rejectSingleMultiInputLifecycleMutation(ctx, reservation); err != nil {
		return nil, err
	}
	now := s.now()
	var operationUpdate *PayrollOperation
	if reservation.OperationID != "" {
		operation, err := s.Store.GetOperation(ctx, reservation.OperationID)
		if err != nil {
			return nil, err
		}
		operationStatus := OperationStatusUnknown
		switch to {
		case StatusManualReview:
			operationStatus = OperationStatusManualReview
		case StatusReplanRequired:
			operationStatus = OperationStatusReplanRequired
		}
		operationUpdate, err = lifecycleOperationUpdate(operation, operationStatus, now)
		if err != nil {
			return nil, err
		}
	}
	updated, _, err := s.Store.ApplyLeaseExpiryRecovery(ctx, ReconciliationTransition{
		ReservationID:                     reservationID,
		From:                              from,
		To:                                to,
		Operation:                         operationUpdate,
		Now:                               now,
		serviceAuthorized:                 true,
		requireSingleReservationOperation: true,
	})
	return updated, err
}

// RecoverOperationAfterLeaseExpiry atomically recovers every input reservation
// and the shared operation after all worker leases have expired.
func (s Service) RecoverOperationAfterLeaseExpiry(ctx context.Context, operationID string, reservationIDs []string, from ReservationStatus, to ReservationStatus) ([]NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if !CanRecoverAfterLeaseExpiry(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s is not an expired-lease recovery transition", ErrInvalidTransition, from, to)
	}
	reservations, operation, err := s.loadExactOperationReservationSet(ctx, operationID, reservationIDs, from)
	if err != nil {
		return nil, err
	}
	now := s.now()
	operationStatus := OperationStatusUnknown
	switch to {
	case StatusManualReview:
		operationStatus = OperationStatusManualReview
	case StatusReplanRequired:
		operationStatus = OperationStatusReplanRequired
	}
	operationUpdate, err := lifecycleOperationUpdate(operation, operationStatus, now)
	if err != nil {
		return nil, err
	}
	transition := ReconciliationTransition{
		ReservationID:           reservations[0].ReservationID,
		From:                    from,
		To:                      to,
		Operation:               operationUpdate,
		OperationReservationIDs: append([]string(nil), reservationIDs...),
		Now:                     now,
		serviceAuthorized:       true,
	}
	_, _, err = s.Store.ApplyLeaseExpiryRecovery(ctx, transition)
	if err != nil {
		return nil, err
	}
	return operationTransitionResult(reservations, transition), nil
}

// ResolveManualReview records operator approval and atomically transitions the
// reservation and its linked operation out of ManualReview.
func (s Service) ResolveManualReview(ctx context.Context, reservationID string, resolution ManualReviewResolution) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	resolution.OperatorID = strings.TrimSpace(resolution.OperatorID)
	resolution.ApprovalReference = strings.TrimSpace(resolution.ApprovalReference)
	resolution.Reason = strings.TrimSpace(resolution.Reason)
	if resolution.OperatorID == "" || resolution.ApprovalReference == "" {
		return nil, fmt.Errorf("%w: operator id and approval reference are required", ErrInvalidReservation)
	}
	switch resolution.Target {
	case StatusReleased, StatusReplanRequired, StatusFailed:
	default:
		return nil, fmt.Errorf("%w: ManualReview cannot resolve to %s", ErrInvalidTransition, resolution.Target)
	}
	reservation, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	if reservation.Status != StatusManualReview {
		return nil, fmt.Errorf("%w: expected %s got %s", ErrCompareAndSetFailed, StatusManualReview, reservation.Status)
	}
	if err := s.rejectSingleMultiInputLifecycleMutation(ctx, reservation); err != nil {
		return nil, err
	}

	now := s.now()
	var operationUpdate *PayrollOperation
	if reservation.OperationID != "" {
		operation, err := s.Store.GetOperation(ctx, reservation.OperationID)
		if err != nil {
			return nil, err
		}
		operationStatus := OperationStatusReplanRequired
		if resolution.Target == StatusFailed {
			operationStatus = OperationStatusFailed
		}
		operationUpdate, err = lifecycleOperationUpdate(operation, operationStatus, now)
		if err != nil {
			return nil, err
		}
	}
	updated, _, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:                     reservationID,
		From:                              StatusManualReview,
		To:                                resolution.Target,
		Operation:                         operationUpdate,
		ManualReviewResolution:            &resolution,
		Now:                               now,
		serviceAuthorized:                 true,
		requireSingleReservationOperation: true,
	})
	return updated, err
}

// ResolveManualReviewOperation records one operator decision for the complete
// linked input set and transitions reservations and operation atomically.
func (s Service) ResolveManualReviewOperation(ctx context.Context, operationID string, reservationIDs []string, resolution ManualReviewResolution) ([]NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	resolution.OperatorID = strings.TrimSpace(resolution.OperatorID)
	resolution.ApprovalReference = strings.TrimSpace(resolution.ApprovalReference)
	resolution.Reason = strings.TrimSpace(resolution.Reason)
	if resolution.OperatorID == "" || resolution.ApprovalReference == "" {
		return nil, fmt.Errorf("%w: operator id and approval reference are required", ErrInvalidReservation)
	}
	switch resolution.Target {
	case StatusReleased, StatusReplanRequired, StatusFailed:
	default:
		return nil, fmt.Errorf("%w: ManualReview cannot resolve to %s", ErrInvalidTransition, resolution.Target)
	}
	reservations, operation, err := s.loadExactOperationReservationSet(ctx, operationID, reservationIDs, StatusManualReview)
	if err != nil {
		return nil, err
	}
	now := s.now()
	operationStatus := OperationStatusReplanRequired
	if resolution.Target == StatusFailed {
		operationStatus = OperationStatusFailed
	}
	operationUpdate, err := lifecycleOperationUpdate(operation, operationStatus, now)
	if err != nil {
		return nil, err
	}
	transition := ReconciliationTransition{
		ReservationID:           reservations[0].ReservationID,
		From:                    StatusManualReview,
		To:                      resolution.Target,
		Operation:               operationUpdate,
		ManualReviewResolution:  &resolution,
		OperationReservationIDs: append([]string(nil), reservationIDs...),
		Now:                     now,
		serviceAuthorized:       true,
	}
	_, _, err = s.Store.ApplyReconciliationTransition(ctx, transition)
	if err != nil {
		return nil, err
	}
	return operationTransitionResult(reservations, transition), nil
}

// ReplanProofReadyAfterDiscard releases a live ProofReady worker lease only
// when the caller proves the local proof was discarded before broadcast.
func (s Service) ReplanProofReadyAfterDiscard(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, evidence ProofDiscardEvidence) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if !evidence.NoBroadcastAttempt || !evidence.ProofDiscarded {
		return nil, fmt.Errorf("%w: no-broadcast and proof-discard evidence are required", ErrInvalidReservation)
	}
	reservation, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	if reservation.Status != StatusProofReady {
		return nil, fmt.Errorf("%w: expected %s got %s", ErrCompareAndSetFailed, StatusProofReady, reservation.Status)
	}
	if err := s.rejectSingleMultiInputLifecycleMutation(ctx, reservation); err != nil {
		return nil, err
	}
	now := s.now()
	var operationUpdate *PayrollOperation
	if reservation.OperationID != "" {
		operation, err := s.Store.GetOperation(ctx, reservation.OperationID)
		if err != nil {
			return nil, err
		}
		operationUpdate, err = lifecycleOperationUpdate(operation, OperationStatusReplanRequired, now)
		if err != nil {
			return nil, err
		}
	}
	if _, err := s.Store.MarkReservationsProofDiscarding(ctx, reservation.OperationID, []SubmittedReservationRef{{
		ReservationID: reservationID,
		LeaseOwner:    leaseOwner,
		LeaseToken:    leaseToken,
	}}, now); err != nil {
		return nil, err
	}
	updated, _, err := s.Store.ApplyProofDiscardTransition(ctx, ReconciliationTransition{
		ReservationID:                     reservationID,
		From:                              StatusProofReady,
		To:                                StatusReplanRequired,
		Operation:                         operationUpdate,
		ProofDiscardEvidence:              &evidence,
		LeaseOwner:                        leaseOwner,
		LeaseToken:                        leaseToken,
		Now:                               now,
		serviceAuthorized:                 true,
		requireSingleReservationOperation: true,
	})
	return updated, err
}

// ReplanProofReadyOperationAfterDiscard atomically discards the complete proof
// input set. Each reservation must still hold the supplied live lease.
func (s Service) ReplanProofReadyOperationAfterDiscard(ctx context.Context, operationID string, refs []SubmittedReservationRef, evidence ProofDiscardEvidence) ([]NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if !evidence.NoBroadcastAttempt || !evidence.ProofDiscarded {
		return nil, fmt.Errorf("%w: no-broadcast and proof-discard evidence are required", ErrInvalidReservation)
	}
	reservationIDs := make([]string, 0, len(refs))
	for _, ref := range refs {
		reservationIDs = append(reservationIDs, ref.ReservationID)
	}
	reservations, operation, err := s.loadExactOperationReservationSet(ctx, operationID, reservationIDs, StatusProofReady)
	if err != nil {
		return nil, err
	}
	now := s.now()
	operationUpdate, err := lifecycleOperationUpdate(operation, OperationStatusReplanRequired, now)
	if err != nil {
		return nil, err
	}
	if _, err := s.Store.MarkReservationsProofDiscarding(ctx, operationID, refs, now); err != nil {
		return nil, err
	}
	transition := ReconciliationTransition{
		ReservationID:            reservations[0].ReservationID,
		From:                     StatusProofReady,
		To:                       StatusReplanRequired,
		Operation:                operationUpdate,
		ProofDiscardEvidence:     &evidence,
		OperationReservationIDs:  append([]string(nil), reservationIDs...),
		OperationReservationRefs: append([]SubmittedReservationRef(nil), refs...),
		Now:                      now,
		serviceAuthorized:        true,
	}
	_, _, err = s.Store.ApplyProofDiscardTransition(ctx, transition)
	if err != nil {
		return nil, err
	}
	return operationTransitionResult(reservations, transition), nil
}

// BeginProofDiscardOperation durably blocks publish and broadcast before an
// outbox removes the exact ProofReady artifact set. Repeating the call with the
// same live leases is idempotent so crash recovery can resume the discard.
func (s Service) BeginProofDiscardOperation(ctx context.Context, operationID string, refs []SubmittedReservationRef) ([]NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	reservationIDs := make([]string, 0, len(refs))
	for _, ref := range refs {
		reservationIDs = append(reservationIDs, ref.ReservationID)
	}
	if _, _, err := s.loadExactOperationReservationSet(ctx, operationID, reservationIDs, StatusProofReady); err != nil {
		return nil, err
	}
	return s.Store.MarkReservationsProofDiscarding(ctx, operationID, refs, s.now())
}

func (s Service) loadExactOperationReservationSet(ctx context.Context, operationID string, reservationIDs []string, status ReservationStatus) ([]NoteReservation, *PayrollOperation, error) {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" || len(reservationIDs) == 0 {
		return nil, nil, fmt.Errorf("%w: operation_id and reservation ids are required", ErrInvalidReservation)
	}
	provided := make(map[string]struct{}, len(reservationIDs))
	for _, reservationID := range reservationIDs {
		if strings.TrimSpace(reservationID) == "" {
			return nil, nil, fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
		}
		if _, duplicate := provided[reservationID]; duplicate {
			return nil, nil, fmt.Errorf("%w: duplicate reservation_id %s", ErrInvalidReservation, reservationID)
		}
		provided[reservationID] = struct{}{}
	}
	all, err := s.Store.ListReservations(ctx, ReservationFilter{})
	if err != nil {
		return nil, nil, err
	}
	linked := make(map[string]NoteReservation)
	for _, reservation := range all {
		if reservation.OperationID == operationID {
			linked[reservation.ReservationID] = reservation
		}
	}
	if len(linked) != len(provided) {
		return nil, nil, fmt.Errorf("%w: operation %s requires its complete reservation set", ErrInvalidReservation, operationID)
	}
	ordered := make([]NoteReservation, 0, len(reservationIDs))
	for _, reservationID := range reservationIDs {
		reservation, ok := linked[reservationID]
		if !ok || reservation.Status != status {
			return nil, nil, fmt.Errorf("%w: invalid %s reservation %s for operation %s", ErrCompareAndSetFailed, status, reservationID, operationID)
		}
		ordered = append(ordered, reservation)
	}
	operation, err := s.Store.GetOperation(ctx, operationID)
	if err != nil {
		return nil, nil, err
	}
	return ordered, operation, nil
}

func operationTransitionResult(reservations []NoteReservation, transition ReconciliationTransition) []NoteReservation {
	updated := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		candidate := cloneReservation(reservation)
		candidate.Status = transition.To
		clearLeaseForStatusTransition(&candidate, transition.To)
		if resolution := transition.ManualReviewResolution; resolution != nil {
			candidate.ManualReviewResolvedBy = strings.TrimSpace(resolution.OperatorID)
			candidate.ManualReviewApprovalReference = strings.TrimSpace(resolution.ApprovalReference)
			candidate.ManualReviewResolutionReason = strings.TrimSpace(resolution.Reason)
		}
		candidate.UpdatedAt = transition.Now
		updated = append(updated, candidate)
	}
	return updated
}

func (s Service) rejectSingleMultiInputLifecycleMutation(ctx context.Context, reservation *NoteReservation) error {
	if reservation == nil || reservation.OperationID == "" {
		return nil
	}
	reservations, err := s.Store.ListReservations(ctx, ReservationFilter{})
	if err != nil {
		return err
	}
	linked := 0
	for _, candidate := range reservations {
		if candidate.OperationID != reservation.OperationID {
			continue
		}
		linked++
		if linked > 1 {
			return fmt.Errorf("%w: multi-input operation %s requires an atomic recovery command", ErrInvalidTransition, reservation.OperationID)
		}
	}
	return nil
}

func lifecycleOperationUpdate(operation *PayrollOperation, target OperationStatus, now time.Time) (*PayrollOperation, error) {
	if operation == nil {
		return nil, nil
	}
	switch operation.Status {
	case OperationStatusSucceeded, OperationStatusConflictSpent:
		return nil, fmt.Errorf("%w: linked operation is terminal (%s)", ErrManualReviewRequired, operation.Status)
	case OperationStatusFailed:
		// A failed operation stays an audit record while its note is manually
		// released or replanned under a new operation id.
		return nil, nil
	}
	updated := *operation
	updated.Status = target
	updated.UpdatedAt = now
	return &updated, nil
}

func (s Service) MarkSubmitted(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, txHash string, txBytesHash string, signDocHash string, accountSequence uint64) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	reservation, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	var operationIDs []string
	if reservation.OperationID != "" {
		operationIDs = []string{reservation.OperationID}
	}
	updated, _, err := s.Store.MarkReservationsSubmitted(ctx, []SubmittedReservationRef{{
		ReservationID: reservationID,
		LeaseOwner:    leaseOwner,
		LeaseToken:    leaseToken,
	}}, operationIDs, SubmittedReservationUpdate{
		TxHash:          txHash,
		TxBytesHash:     txBytesHash,
		SignDocHash:     signDocHash,
		AccountSequence: accountSequence,
	}, s.now())
	if err != nil {
		return nil, err
	}
	return &updated[0], nil
}

func (s Service) HeartbeatLeaseForStatus(ctx context.Context, reservationID string, owner string, token string, status ReservationStatus, ttl time.Duration) (*Lease, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("lease ttl must be positive")
	}
	now := s.now()
	updated, err := s.Store.HeartbeatReservationLeaseForStatus(ctx, reservationID, owner, token, status, now.Add(ttl), now)
	if err != nil {
		return nil, err
	}
	return &Lease{
		Owner: updated.LeaseOwner,
		Token: updated.LeaseToken,
		Until: updated.LeaseUntil,
	}, nil
}

// RecordRelayHandoff makes an externally copied relay payload durable before
// local UI cleanup. The current ProofReady lease proves the caller owns it.
func (s Service) RecordRelayHandoff(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, payloadHash string) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.RecordRelayHandoff(ctx, reservationID, leaseOwner, leaseToken, payloadHash, s.now())
}

// RecordRelayHandoffBatch atomically marks every ProofReady input of an
// operation as handed off. The Store rejects partial, mixed-status, or
// payload-hash-mismatched batches.
func (s Service) RecordRelayHandoffBatch(ctx context.Context, operationID string, refs []SubmittedReservationRef, payloadHash string) ([]NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.RecordRelayHandoffBatch(ctx, operationID, refs, payloadHash, s.now())
}

func (s Service) MarkSubmittedBatch(ctx context.Context, refs []SubmittedReservationRef, operationIDs []string, update SubmittedReservationUpdate) ([]NoteReservation, []PayrollOperation, error) {
	if s.Store == nil {
		return nil, nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.MarkReservationsSubmitted(ctx, refs, operationIDs, update, s.now())
}

func (s Service) MarkBroadcastUnknownBatch(ctx context.Context, refs []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptUpdate) ([]NoteReservation, []PayrollOperation, error) {
	if s.Store == nil {
		return nil, nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.MarkReservationsBroadcastUnknown(ctx, refs, operationIDs, update, s.now())
}

// MarkBroadcastAmbiguousBatch fail-closes an unidentifiable post-boundary
// submission error. Without a durable tx identity, retrying could double-send.
func (s Service) MarkBroadcastAmbiguousBatch(ctx context.Context, refs []SubmittedReservationRef, operationIDs []string, update BroadcastAmbiguityUpdate) ([]NoteReservation, []PayrollOperation, error) {
	if s.Store == nil {
		return nil, nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.MarkReservationsBroadcastAmbiguous(ctx, refs, operationIDs, update, s.now())
}

// MarkProofArtifactCleanupFailedBatch fail-closes a proving batch when its
// staged proof cannot be proven deleted after a failed ProofReady transition.
// The active ManualReview state prevents a new worker from producing a second
// artifact for the same input notes.
func (s Service) MarkProofArtifactCleanupFailedBatch(ctx context.Context, refs []SubmittedReservationRef, operationIDs []string, reason string) ([]NoteReservation, []PayrollOperation, error) {
	if s.Store == nil {
		return nil, nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.MarkReservationsProofArtifactCleanupFailed(ctx, refs, operationIDs, reason, s.now())
}

func (s Service) MarkProofReadyBatch(ctx context.Context, refs []SubmittedReservationRef, update ProofReadyOperationUpdate) ([]NoteReservation, *PayrollOperation, error) {
	if s.Store == nil {
		return nil, nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.MarkReservationsProofReady(ctx, refs, update, s.now())
}

// MarkBroadcastAttempting durably closes the retry gate, together with the
// signed transaction identity, before an external broadcaster is invoked. A
// ProofReady attempt left in-flight must be reconciled; it cannot be submitted
// again.
func (s Service) MarkBroadcastAttempting(ctx context.Context, refs []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptStart) ([]NoteReservation, []PayrollOperation, error) {
	if s.Store == nil {
		return nil, nil, fmt.Errorf("reservation store is required")
	}
	if strings.TrimSpace(update.TxHash) == "" && strings.TrimSpace(update.TxBytesHash) == "" {
		return nil, nil, fmt.Errorf("%w: broadcast attempt requires tx_hash or tx_bytes_hash", ErrInvalidReservation)
	}
	return s.Store.MarkReservationsBroadcastAttempting(ctx, refs, operationIDs, update, s.now())
}

func (s Service) Release(ctx context.Context, reservationID string, from ReservationStatus) (*NoteReservation, error) {
	if from != StatusReserved {
		return nil, fmt.Errorf("%w: automatic release is only allowed from Reserved", ErrInvalidTransition)
	}
	return s.Transition(ctx, reservationID, from, StatusReleased)
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
