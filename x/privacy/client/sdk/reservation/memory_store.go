package reservation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu                     sync.Mutex
	reservations           map[string]NoteReservation
	operations             map[string]PayrollOperation
	batchOperations        map[string]BatchOperation
	batchInputs            map[string][]OperationInputReservation
	batchItems             map[string][]PayrollItemOutput
	batchEvidence          map[string][]ExpectedOutputEvidence
	activeReservationByKey map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		reservations:           make(map[string]NoteReservation),
		operations:             make(map[string]PayrollOperation),
		batchOperations:        make(map[string]BatchOperation),
		batchInputs:            make(map[string][]OperationInputReservation),
		batchItems:             make(map[string][]PayrollItemOutput),
		batchEvidence:          make(map[string][]ExpectedOutputEvidence),
		activeReservationByKey: make(map[string]string),
	}
}

func (s *MemoryStore) CreateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error) {
	created, err := s.CreateReservationBatch(ctx, []NoteReservation{reservation}, nil)
	if err != nil {
		return nil, err
	}
	return &created[0], nil
}

func (s *MemoryStore) CreateReservationBatch(_ context.Context, reservations []NoteReservation, operations []PayrollOperation) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(reservations) == 0 {
		return []NoteReservation{}, nil
	}
	s.ensureMapsLocked()
	pendingReservations := make(map[string]NoteReservation, len(reservations))
	pendingActiveKeys := make(map[string]string, len(reservations))
	pendingOperations := make(map[string]PayrollOperation, len(operations))
	for _, reservation := range reservations {
		if err := s.validateReservationCreateLocked(reservation, pendingReservations, pendingActiveKeys); err != nil {
			return nil, err
		}
		pendingReservations[reservation.ReservationID] = reservation
	}
	for _, operation := range operations {
		if err := s.validateOperationCreateLocked(operation, pendingOperations); err != nil {
			return nil, err
		}
		pendingOperations[operation.OperationID] = operation
	}
	if err := s.validateReservationOperationCreateLinksLocked(reservations, pendingReservations, pendingOperations); err != nil {
		return nil, err
	}

	created := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		s.storeReservationLocked(reservation)
		created = append(created, cloneReservation(reservation))
	}
	for _, operation := range operations {
		s.operations[operation.OperationID] = cloneOperation(operation)
	}
	return created, nil
}

func (s *MemoryStore) validateReservationCreateLocked(reservation NoteReservation, pending map[string]NoteReservation, pendingActiveKeys map[string]string) error {
	if err := validateInitialReservationState(reservation); err != nil {
		return err
	}
	if reservation.ReservationID == "" {
		return fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
	}
	if _, exists := s.reservations[reservation.ReservationID]; exists {
		return fmt.Errorf("%w: reservation_id already exists", ErrActiveReservationExists)
	}
	if _, exists := pending[reservation.ReservationID]; exists {
		return fmt.Errorf("%w: reservation_id already exists in batch", ErrActiveReservationExists)
	}
	if s.confirmedSpentReservationExistsLocked(reservation, "") {
		return fmt.Errorf("%w: note is already confirmed spent", ErrInvalidReservation)
	}
	if IsActiveReservationStatus(reservation.Status) {
		activeKey := reservation.ActiveKey()
		if _, exists := s.activeReservationByKey[activeKey]; exists {
			return ErrActiveReservationExists
		}
		if _, exists := pendingActiveKeys[activeKey]; exists {
			return ErrActiveReservationExists
		}
		pendingActiveKeys[activeKey] = reservation.ReservationID
	}
	return nil
}

func (s *MemoryStore) validateOperationCreateLocked(operation PayrollOperation, pending map[string]PayrollOperation) error {
	if err := validateInitialOperationState(operation); err != nil {
		return err
	}
	if operation.OperationID == "" {
		return fmt.Errorf("operation_id is required")
	}
	if _, exists := s.operations[operation.OperationID]; exists {
		return fmt.Errorf("operation already exists")
	}
	if _, exists := pending[operation.OperationID]; exists {
		return fmt.Errorf("operation already exists in batch")
	}
	return nil
}

func validateInitialReservationState(reservation NoteReservation) error {
	if reservation.Status != StatusReserved {
		return fmt.Errorf("%w: new reservations must start as Reserved", ErrInvalidReservation)
	}
	if reservation.LeaseOwner != "" || reservation.LeaseToken != "" || !reservation.LeaseUntil.IsZero() || !reservation.LastHeartbeatAt.IsZero() ||
		reservation.PayloadHash != "" || reservation.SignDocHash != "" || reservation.TxBytesHash != "" || reservation.TxHash != "" || reservation.AccountSequence != 0 ||
		reservation.BroadcastAttemptCount != 0 || reservation.BroadcastInFlight || reservation.ProofDiscardInFlight || !reservation.ProofDiscardStartedAt.IsZero() || !reservation.LastBroadcastAt.IsZero() || reservation.LastBroadcastError != "" ||
		reservation.RelayHandedOff || !reservation.RelayHandedOffAt.IsZero() || reservation.ReconciliationReviewReason != "" || !reservation.LastReconciledAt.IsZero() ||
		reservation.ManualReviewResolvedBy != "" || reservation.ManualReviewApprovalReference != "" || reservation.ManualReviewResolutionReason != "" {
		return fmt.Errorf("%w: new reservations cannot include lifecycle, broadcast, relay, or reconciliation evidence", ErrInvalidReservation)
	}
	return nil
}

func validateInitialOperationState(operation PayrollOperation) error {
	if operation.Status != OperationStatusPlanned {
		return fmt.Errorf("%w: operation must start as Planned", ErrInvalidReservation)
	}
	if operation.PayloadHash != "" || operation.SignDocHash != "" || operation.TxBytesHash != "" || operation.TxHash != "" {
		return fmt.Errorf("%w: operation cannot include transaction or payload identity at creation", ErrInvalidReservation)
	}
	return nil
}

func (s *MemoryStore) validateReservationOperationCreateLinksLocked(reservations []NoteReservation, pendingReservations map[string]NoteReservation, pendingOperations map[string]PayrollOperation) error {
	checkedExistingOperations := make(map[string]struct{})
	for _, reservation := range reservations {
		if reservation.OperationID == "" {
			continue
		}
		_, pending := pendingOperations[reservation.OperationID]
		operation, exists := s.operations[reservation.OperationID]
		if !pending && !exists {
			return fmt.Errorf("%w: reservation %s references missing operation %s", ErrOperationNotFound, reservation.ReservationID, reservation.OperationID)
		}
		if !pending {
			if _, checked := checkedExistingOperations[reservation.OperationID]; !checked {
				linked := make([]NoteReservation, 0)
				for _, existingReservation := range s.reservations {
					if existingReservation.OperationID == reservation.OperationID {
						linked = append(linked, existingReservation)
					}
				}
				if err := validateOperationMembershipExtension(operation, linked); err != nil {
					return err
				}
				checkedExistingOperations[reservation.OperationID] = struct{}{}
			}
		}
		// A single operation can consume several input reservations. The
		// operation's legacy ReservationID, when present, names its primary
		// reservation; sibling inputs are linked through OperationID.
	}
	for _, operation := range pendingOperations {
		if operation.ReservationID == "" {
			continue
		}
		reservation, exists := pendingReservations[operation.ReservationID]
		if !exists {
			reservation, exists = s.reservations[operation.ReservationID]
		}
		if !exists || reservation.OperationID != operation.OperationID {
			return fmt.Errorf("%w: operation %s references missing or mismatched reservation %s", ErrInvalidReservation, operation.OperationID, operation.ReservationID)
		}
	}
	return nil
}

// UnsafeImportReservationForTesting bypasses normal lifecycle creation checks.
// It exists only for test fixtures and migrations that reconstruct past state.
func (s *MemoryStore) UnsafeImportReservationForTesting(_ context.Context, reservation NoteReservation) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()
	if reservation.ReservationID == "" {
		return nil, fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
	}
	if _, exists := s.reservations[reservation.ReservationID]; exists {
		return nil, fmt.Errorf("%w: reservation_id already exists", ErrActiveReservationExists)
	}
	s.storeReservationLocked(reservation)
	cloned := cloneReservation(reservation)
	return &cloned, nil
}

func (s *MemoryStore) GetReservation(_ context.Context, reservationID string) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	cloned := cloneReservation(reservation)
	return &cloned, nil
}

// UpdateReservation is retained for the SQL and durable-file adapters. Normal
// lifecycle code should use the evidence-aware transition methods instead.
func (s *MemoryStore) UpdateReservation(_ context.Context, reservation NoteReservation) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()
	if _, ok := s.reservations[reservation.ReservationID]; !ok {
		return nil, ErrReservationNotFound
	}
	s.storeReservationLocked(reservation)
	updated := cloneReservation(reservation)
	return &updated, nil
}

func (s *MemoryStore) ListReservations(_ context.Context, filter ReservationFilter) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	statuses := make(map[ReservationStatus]struct{}, len(filter.Statuses))
	for _, status := range filter.Statuses {
		statuses[status] = struct{}{}
	}

	out := make([]NoteReservation, 0, len(s.reservations))
	for _, reservation := range s.reservations {
		if filter.OperationID != "" && reservation.OperationID != filter.OperationID {
			continue
		}
		if len(statuses) > 0 {
			if _, ok := statuses[reservation.Status]; !ok {
				continue
			}
		}
		out = append(out, cloneReservation(reservation))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryStore) CompareAndSetReservationStatus(_ context.Context, reservationID string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if RequiresLeaseToken(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s requires lease token", ErrLeaseMismatch, from, to)
	}
	if requiresManagedReservationTransition(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s requires a dedicated evidence-aware transition", ErrInvalidTransition, from, to)
	}
	return s.compareAndSetReservationStatusLocked(reservationID, from, to, now)
}

func (s *MemoryStore) ApplyReconciliationTransition(_ context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	if !transition.ServiceAuthorized() {
		return nil, nil, fmt.Errorf("%w: reconciliation transition must be authorized by Service", ErrInvalidTransition)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyReconciliationTransitionLocked(transition)
}

func (s *MemoryStore) applyReconciliationTransitionLocked(transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	s.ensureMapsLocked()
	if len(transition.OperationReservationIDs) > 0 && !transition.QuarantinesMatchingSpent() {
		return s.applyLifecycleOperationTransitionLocked(transition)
	}

	reservation, ok := s.reservations[transition.ReservationID]
	if !ok {
		return nil, nil, ErrReservationNotFound
	}
	if transition.RequiresSingleReservationOperation() && reservation.OperationID != "" {
		linked := 0
		for _, candidate := range s.reservations {
			if candidate.OperationID == reservation.OperationID {
				linked++
				if linked > 1 {
					return nil, nil, fmt.Errorf("%w: multi-input operation %s requires an atomic operation command", ErrInvalidTransition, reservation.OperationID)
				}
			}
		}
	}
	if reservation.Status != transition.From {
		return nil, nil, ErrCompareAndSetFailed
	}
	quarantineSpent := transition.QuarantinesMatchingSpent() &&
		transition.To == StatusConfirmedSpent
	isTerminalAudit := transition.From == transition.To &&
		IsTerminalReservationStatus(transition.From) &&
		strings.TrimSpace(transition.AuditReason) != ""
	if !quarantineSpent && !isTerminalAudit && !CanTransitionReservation(transition.From, transition.To) {
		return nil, nil, ErrInvalidTransition
	}
	candidate := reservation
	candidate.Status = transition.To
	if err := mergeReconciliationIdentityIntoReservation(&candidate, transition); err != nil {
		return nil, nil, err
	}
	if IsActiveReservationStatus(transition.To) && (s.activeReservationConflictLocked(candidate, transition.ReservationID) || s.confirmedSpentReservationExistsLocked(candidate, transition.ReservationID)) {
		return nil, nil, ErrActiveReservationExists
	}
	if !isTerminalAudit {
		clearLeaseForStatusTransition(&candidate, transition.To)
	}
	if isTerminalAudit {
		candidate.ReconciliationReviewReason = strings.TrimSpace(transition.AuditReason)
		candidate.LastReconciledAt = transition.Now
	}
	if resolution := transition.ManualReviewResolution; resolution != nil {
		operatorID := strings.TrimSpace(resolution.OperatorID)
		approvalReference := strings.TrimSpace(resolution.ApprovalReference)
		if transition.From != StatusManualReview || resolution.Target != transition.To || operatorID == "" || approvalReference == "" {
			return nil, nil, fmt.Errorf("%w: invalid manual review resolution", ErrInvalidReservation)
		}
		candidate.ManualReviewResolvedBy = operatorID
		candidate.ManualReviewApprovalReference = approvalReference
		candidate.ManualReviewResolutionReason = strings.TrimSpace(resolution.Reason)
	}

	var operationUpdate *PayrollOperation
	if transition.Operation != nil {
		if transition.Operation.OperationID == "" {
			return nil, nil, fmt.Errorf("%w: operation_id is required", ErrInvalidReservation)
		}
		existing, ok := s.operations[transition.Operation.OperationID]
		if !ok {
			return nil, nil, ErrOperationNotFound
		}
		if reservation.OperationID != "" && reservation.OperationID != transition.Operation.OperationID {
			return nil, nil, fmt.Errorf("%w: reservation %s belongs to operation %s", ErrInvalidReservation, transition.ReservationID, reservation.OperationID)
		}
		if err := validateOperationStatusAdvance(existing, transition.Operation.Status); err != nil {
			return nil, nil, err
		}
		if IsTerminalOperationStatus(existing.Status) {
		} else {
			cloned := cloneOperation(*transition.Operation)
			cloned.ReservationID = existing.ReservationID
			if existing.ReservationID == "" {
				cloned.ReservationID = transition.ReservationID
			}
			operationUpdate = &cloned
		}
	}

	quarantinedReservationIDs := map[string]struct{}{}
	if quarantineSpent {
		quarantinedReservationIDs[transition.ReservationID] = struct{}{}
		if len(transition.OperationReservationIDs) > 0 {
			if transition.Operation == nil || transition.Operation.Status != OperationStatusSucceeded || reservation.OperationID == "" || reservation.OperationID != transition.Operation.OperationID {
				return nil, nil, fmt.Errorf("%w: operation reconciliation requires a matching succeeded operation", ErrInvalidReservation)
			}
			providedIDs := make(map[string]struct{}, len(transition.OperationReservationIDs))
			for _, reservationID := range transition.OperationReservationIDs {
				if reservationID == "" {
					return nil, nil, fmt.Errorf("%w: operation reconciliation reservation_id is required", ErrInvalidReservation)
				}
				if _, duplicate := providedIDs[reservationID]; duplicate {
					return nil, nil, fmt.Errorf("%w: duplicate operation reconciliation reservation %s", ErrInvalidReservation, reservationID)
				}
				sibling, exists := s.reservations[reservationID]
				if !exists || sibling.OperationID != reservation.OperationID || !canReconcileSpent(sibling.Status) {
					return nil, nil, fmt.Errorf("%w: invalid operation reconciliation reservation %s", ErrInvalidReservation, reservationID)
				}
				providedIDs[reservationID] = struct{}{}
			}
			for reservationID, sibling := range s.reservations {
				if sibling.OperationID != reservation.OperationID {
					continue
				}
				if _, included := providedIDs[reservationID]; !included {
					return nil, nil, fmt.Errorf("%w: operation reconciliation is missing reservation %s", ErrInvalidReservation, reservationID)
				}
				quarantinedReservationIDs[reservationID] = struct{}{}
			}
		}
		for reservationID := range quarantinedReservationIDs {
			key := s.reservations[reservationID].ActiveKey()
			for siblingID, sibling := range s.reservations {
				if sibling.ActiveKey() == key {
					quarantinedReservationIDs[siblingID] = struct{}{}
				}
			}
		}
	}

	// A spent-nullifier quarantine may touch reservations from earlier or
	// concurrent operations. Update every non-terminal linked operation in the
	// same store lock so a scheduler cannot keep retrying a sibling after its
	// input note has been permanently quarantined.
	siblingOperationUpdates := make(map[string]PayrollOperation)
	if quarantineSpent {
		siblingTarget := transition.SiblingOperationStatus
		if siblingTarget == "" {
			siblingTarget = OperationStatusConflictSpent
		}
		for reservationID, sibling := range s.reservations {
			if _, quarantined := quarantinedReservationIDs[reservationID]; !quarantined || sibling.OperationID == "" {
				continue
			}
			existing, ok := s.operations[sibling.OperationID]
			if !ok {
				return nil, nil, ErrOperationNotFound
			}
			if IsTerminalOperationStatus(existing.Status) {
				continue
			}
			updated := cloneOperation(existing)
			updated.Status = siblingTarget
			updated.UpdatedAt = transition.Now
			siblingOperationUpdates[updated.OperationID] = updated
		}
		if operationUpdate != nil {
			siblingOperationUpdates[operationUpdate.OperationID] = cloneOperation(*operationUpdate)
		}
		for _, updated := range siblingOperationUpdates {
			if err := validateOperationStatusAdvance(s.operations[updated.OperationID], updated.Status); err != nil {
				return nil, nil, err
			}
		}
	}

	candidate.UpdatedAt = transition.Now
	if quarantineSpent {
		for reservationID, sibling := range s.reservations {
			if _, quarantined := quarantinedReservationIDs[reservationID]; !quarantined {
				continue
			}
			sibling.Status = StatusConfirmedSpent
			clearReservationLeaseFields(&sibling)
			sibling.ReconciliationReviewReason = strings.TrimSpace(transition.AuditReason)
			sibling.LastReconciledAt = transition.Now
			sibling.UpdatedAt = transition.Now
			s.storeReservationLocked(sibling)
			if reservationID == candidate.ReservationID {
				candidate = sibling
			}
		}
		for operationID, updated := range siblingOperationUpdates {
			s.operations[operationID] = updated
		}
	} else {
		s.storeReservationLocked(candidate)
	}
	if !quarantineSpent && operationUpdate != nil {
		s.operations[operationUpdate.OperationID] = *operationUpdate
	}

	clonedReservation := cloneReservation(candidate)
	var clonedOperation *PayrollOperation
	if operationUpdate != nil {
		if updated, ok := siblingOperationUpdates[operationUpdate.OperationID]; ok {
			operationUpdate = &updated
		}
		cloned := cloneOperation(*operationUpdate)
		clonedOperation = &cloned
	}
	return &clonedReservation, clonedOperation, nil
}

func (s *MemoryStore) applyLifecycleOperationTransitionLocked(transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	operationID := ""
	if transition.Operation != nil {
		operationID = strings.TrimSpace(transition.Operation.OperationID)
	}
	if operationID == "" {
		primary, ok := s.reservations[transition.ReservationID]
		if !ok {
			return nil, nil, ErrReservationNotFound
		}
		operationID = strings.TrimSpace(primary.OperationID)
	}
	if operationID == "" {
		return nil, nil, fmt.Errorf("%w: linked operation is required", ErrInvalidReservation)
	}
	provided := make(map[string]SubmittedReservationRef, len(transition.OperationReservationIDs))
	if transition.OperationReservationFromStatuses != nil &&
		len(transition.OperationReservationFromStatuses) != len(transition.OperationReservationIDs) {
		return nil, nil, fmt.Errorf("%w: operation source status set must be exact", ErrInvalidReservation)
	}
	refByID := make(map[string]SubmittedReservationRef, len(transition.OperationReservationRefs))
	for _, ref := range transition.OperationReservationRefs {
		refByID[ref.ReservationID] = ref
	}
	for _, reservationID := range transition.OperationReservationIDs {
		if reservationID == "" {
			return nil, nil, fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
		}
		if _, duplicate := provided[reservationID]; duplicate {
			return nil, nil, fmt.Errorf("%w: duplicate reservation_id %s", ErrInvalidReservation, reservationID)
		}
		provided[reservationID] = refByID[reservationID]
	}
	for reservationID, reservation := range s.reservations {
		if reservation.OperationID != operationID {
			continue
		}
		if _, included := provided[reservationID]; !included {
			return nil, nil, fmt.Errorf("%w: operation %s is missing reservation %s", ErrInvalidReservation, operationID, reservationID)
		}
	}
	if len(provided) == 0 {
		return nil, nil, fmt.Errorf("%w: operation reservation set is required", ErrInvalidReservation)
	}

	existingOperation, ok := s.operations[operationID]
	if !ok {
		return nil, nil, ErrOperationNotFound
	}
	if transition.Operation != nil {
		if err := validateOperationStatusAdvance(existingOperation, transition.Operation.Status); err != nil {
			return nil, nil, err
		}
	}
	candidates := make([]NoteReservation, 0, len(transition.OperationReservationIDs))
	for _, reservationID := range transition.OperationReservationIDs {
		reservation, exists := s.reservations[reservationID]
		if !exists {
			return nil, nil, ErrReservationNotFound
		}
		expectedFrom := transition.From
		if transition.OperationReservationFromStatuses != nil {
			var included bool
			expectedFrom, included = transition.OperationReservationFromStatuses[reservationID]
			if !included {
				return nil, nil, fmt.Errorf("%w: operation transition is missing source status for %s", ErrInvalidReservation, reservationID)
			}
		}
		if reservation.OperationID != operationID || reservation.Status != expectedFrom {
			return nil, nil, ErrCompareAndSetFailed
		}
		if expectedFrom != transition.To && !CanTransitionReservation(expectedFrom, transition.To) {
			return nil, nil, ErrInvalidTransition
		}
		if transition.ProofDiscardEvidence != nil {
			ref, ok := refByID[reservationID]
			if !ok {
				return nil, nil, fmt.Errorf("%w: lease ref is required for %s", ErrInvalidReservation, reservationID)
			}
			if err := requireLeaseToken(reservation, ref.LeaseOwner, ref.LeaseToken, transition.Now); err != nil {
				return nil, nil, err
			}
			if reservation.RelayHandedOff || reservation.BroadcastAttemptCount != 0 || reservation.BroadcastInFlight {
				return nil, nil, fmt.Errorf("%w: reservation has handoff or broadcast evidence", ErrInvalidReservation)
			}
			if !reservation.ProofDiscardInFlight {
				return nil, nil, fmt.Errorf("%w: proof discard was not durably started", ErrInvalidReservation)
			}
		}
		candidate := reservation
		candidate.Status = transition.To
		if err := mergeReconciliationIdentityIntoReservation(&candidate, transition); err != nil {
			return nil, nil, err
		}
		if IsActiveReservationStatus(transition.To) &&
			(s.activeReservationConflictLocked(candidate, reservationID) ||
				s.confirmedSpentReservationExistsLocked(candidate, reservationID)) {
			return nil, nil, ErrActiveReservationExists
		}
		clearLeaseForStatusTransition(&candidate, transition.To)
		if strings.TrimSpace(transition.AuditReason) != "" {
			candidate.ReconciliationReviewReason = strings.TrimSpace(transition.AuditReason)
			candidate.LastReconciledAt = transition.Now
		}
		if resolution := transition.ManualReviewResolution; resolution != nil {
			operatorID := strings.TrimSpace(resolution.OperatorID)
			approvalReference := strings.TrimSpace(resolution.ApprovalReference)
			if transition.From != StatusManualReview || resolution.Target != transition.To || operatorID == "" || approvalReference == "" {
				return nil, nil, fmt.Errorf("%w: invalid manual review resolution", ErrInvalidReservation)
			}
			candidate.ManualReviewResolvedBy = operatorID
			candidate.ManualReviewApprovalReference = approvalReference
			candidate.ManualReviewResolutionReason = strings.TrimSpace(resolution.Reason)
		}
		candidate.UpdatedAt = transition.Now
		candidates = append(candidates, candidate)
	}

	operationUpdate := existingOperation
	if transition.Operation != nil {
		operationUpdate = cloneOperation(*transition.Operation)
		operationUpdate.ReservationID = existingOperation.ReservationID
	}
	if err := mergeReconciliationIdentityIntoOperation(&operationUpdate, transition); err != nil {
		return nil, nil, err
	}
	for _, candidate := range candidates {
		s.storeReservationLocked(candidate)
	}
	if transition.Operation != nil {
		s.operations[operationID] = operationUpdate
	}
	primary := cloneReservation(candidates[0])
	clonedOperation := cloneOperation(operationUpdate)
	return &primary, &clonedOperation, nil
}

func mergeReconciliationIdentityIntoReservation(candidate *NoteReservation, transition ReconciliationTransition) error {
	var err error
	if candidate.TxHash, err = mergeExpectedProofReadyValue("tx_hash", candidate.TxHash, transition.TxHash); err != nil {
		return err
	}
	if candidate.TxBytesHash, err = mergeExpectedProofReadyValue("tx_bytes_hash", candidate.TxBytesHash, transition.TxBytesHash); err != nil {
		return err
	}
	if candidate.SignDocHash, err = mergeExpectedProofReadyValue("sign_doc_hash", candidate.SignDocHash, transition.SignDocHash); err != nil {
		return err
	}
	return nil
}

func mergeReconciliationIdentityIntoOperation(candidate *PayrollOperation, transition ReconciliationTransition) error {
	var err error
	if candidate.TxHash, err = mergeExpectedProofReadyValue("tx_hash", candidate.TxHash, transition.TxHash); err != nil {
		return err
	}
	if candidate.TxBytesHash, err = mergeExpectedProofReadyValue("tx_bytes_hash", candidate.TxBytesHash, transition.TxBytesHash); err != nil {
		return err
	}
	if candidate.SignDocHash, err = mergeExpectedProofReadyValue("sign_doc_hash", candidate.SignDocHash, transition.SignDocHash); err != nil {
		return err
	}
	return nil
}

func (s *MemoryStore) CompareAndSetReservationStatusWithLease(_ context.Context, reservationID string, leaseOwner string, leaseToken string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if err := requireLeaseToken(reservation, leaseOwner, leaseToken, now); err != nil {
		return nil, err
	}
	if s.requiresOperationBatchCommandLocked(reservation, from, to) {
		return nil, fmt.Errorf("%w: multi-input operation %s requires an atomic operation command", ErrInvalidTransition, reservation.OperationID)
	}
	if from == StatusProving && to == StatusProofReady && reservation.OperationID != "" {
		return nil, fmt.Errorf("%w: Proving -> ProofReady requires a payload-bound operation command", ErrInvalidTransition)
	}
	if requiresManagedReservationTransition(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s requires a dedicated evidence-aware transition", ErrInvalidTransition, from, to)
	}
	return s.compareAndSetReservationStatusLocked(reservationID, from, to, now)
}

func (s *MemoryStore) requiresOperationBatchCommandLocked(reservation NoteReservation, from ReservationStatus, to ReservationStatus) bool {
	if reservation.OperationID == "" || !isOperationBatchWorkerTransition(from, to) {
		return false
	}
	linked := 0
	for _, candidate := range s.reservations {
		if candidate.OperationID == reservation.OperationID {
			linked++
			if linked > 1 {
				return true
			}
		}
	}
	return false
}

func (s *MemoryStore) ApplyLeaseExpiryRecovery(_ context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	if !transition.ServiceAuthorized() {
		return nil, nil, fmt.Errorf("%w: lease-expiry recovery must be authorized by Service", ErrInvalidTransition)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if !CanRecoverAfterLeaseExpiry(transition.From, transition.To) {
		return nil, nil, fmt.Errorf("%w: %s -> %s is not an expired-lease recovery transition", ErrInvalidTransition, transition.From, transition.To)
	}
	ids := transition.OperationReservationIDs
	if len(ids) == 0 {
		ids = []string{transition.ReservationID}
	}
	for _, reservationID := range ids {
		reservation, ok := s.reservations[reservationID]
		if !ok {
			return nil, nil, ErrReservationNotFound
		}
		if reservation.Status != transition.From {
			return nil, nil, ErrCompareAndSetFailed
		}
		if reservation.LeaseToken == "" || reservation.LeaseUntil.IsZero() || reservation.LeaseUntil.After(transition.Now) {
			return nil, nil, ErrLeaseUnavailable
		}
	}
	return s.applyReconciliationTransitionLocked(transition)
}

func (s *MemoryStore) ApplyProofDiscardTransition(_ context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	if !transition.ServiceAuthorized() {
		return nil, nil, fmt.Errorf("%w: proof-discard transition must be authorized by Service", ErrInvalidTransition)
	}
	if transition.ProofDiscardEvidence == nil || !transition.ProofDiscardEvidence.NoBroadcastAttempt || !transition.ProofDiscardEvidence.ProofDiscarded {
		return nil, nil, fmt.Errorf("%w: proof-discard transition requires no-broadcast and proof-discard evidence", ErrInvalidReservation)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if transition.From != StatusProofReady || transition.To != StatusReplanRequired {
		return nil, nil, fmt.Errorf("%w: proof-discard transition must be ProofReady -> ReplanRequired", ErrInvalidTransition)
	}
	if len(transition.OperationReservationIDs) == 0 {
		reservation, ok := s.reservations[transition.ReservationID]
		if !ok {
			return nil, nil, ErrReservationNotFound
		}
		if reservation.Status != transition.From {
			return nil, nil, ErrCompareAndSetFailed
		}
		if err := requireLeaseToken(reservation, transition.LeaseOwner, transition.LeaseToken, transition.Now); err != nil {
			return nil, nil, err
		}
		if reservation.RelayHandedOff {
			return nil, nil, fmt.Errorf("%w: relay payload was handed off", ErrInvalidReservation)
		}
		if reservation.BroadcastAttemptCount != 0 || reservation.BroadcastInFlight {
			return nil, nil, fmt.Errorf("%w: reservation has broadcast evidence", ErrInvalidReservation)
		}
		if !reservation.ProofDiscardInFlight {
			return nil, nil, fmt.Errorf("%w: proof discard was not durably started", ErrInvalidReservation)
		}
	}
	return s.applyReconciliationTransitionLocked(transition)
}

func requiresManagedReservationTransition(from ReservationStatus, to ReservationStatus) bool {
	switch {
	case to == StatusSubmitted || to == StatusUnknown || to == StatusConfirmedSpent:
		return true
	case (from == StatusProving || from == StatusProofReady) && (to == StatusReplanRequired || to == StatusManualReview):
		return true
	case (from == StatusSubmitted || from == StatusUnknown) && (to == StatusFailed || to == StatusReplanRequired || to == StatusManualReview):
		return true
	case from == StatusManualReview && (to == StatusFailed || to == StatusReleased || to == StatusReplanRequired):
		return true
	default:
		return false
	}
}

func (s *MemoryStore) compareAndSetReservationStatusLocked(reservationID string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	s.ensureMapsLocked()
	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if reservation.Status != from {
		return nil, ErrCompareAndSetFailed
	}
	if !CanTransitionReservation(from, to) {
		return nil, ErrInvalidTransition
	}
	candidate := reservation
	candidate.Status = to
	if IsActiveReservationStatus(to) && (s.activeReservationConflictLocked(candidate, reservationID) || s.confirmedSpentReservationExistsLocked(candidate, reservationID)) {
		return nil, ErrActiveReservationExists
	}
	clearLeaseForStatusTransition(&candidate, to)
	candidate.UpdatedAt = now
	s.storeReservationLocked(candidate)
	cloned := cloneReservation(candidate)
	return &cloned, nil
}

func (s *MemoryStore) AcquireSingleReservationLease(_ context.Context, reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.acquireReservationLeaseLocked(reservationID, owner, leaseToken, requiredStatus, leaseUntil, now)
}

func (s *MemoryStore) AcquireReservationLease(_ context.Context, reservationID string, owner string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.acquireReservationLeaseLocked(reservationID, owner, leaseToken, "", leaseUntil, now)
}

func (s *MemoryStore) AcquireReservationLeaseForStatus(_ context.Context, reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.acquireReservationLeaseLocked(reservationID, owner, leaseToken, requiredStatus, leaseUntil, now)
}

func (s *MemoryStore) acquireReservationLeaseLocked(reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.ensureMapsLocked()
	owner = strings.TrimSpace(owner)
	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if requiredStatus != "" && reservation.Status != requiredStatus {
		return nil, fmt.Errorf("%w: expected %s got %s", ErrCompareAndSetFailed, requiredStatus, reservation.Status)
	}
	if reservation.Status != StatusReserved {
		return nil, fmt.Errorf("%w: new leases are only available for Reserved reservations", ErrLeaseUnavailable)
	}
	if reservation.OperationID != "" {
		linked := 0
		for _, candidate := range s.reservations {
			if candidate.OperationID == reservation.OperationID {
				linked++
				if linked > 1 {
					return nil, fmt.Errorf("%w: multi-input operation %s requires BeginProvingOperation", ErrLeaseUnavailable, reservation.OperationID)
				}
			}
		}
	}
	if reservation.RelayHandedOff {
		return nil, fmt.Errorf("%w: relay payload was handed off", ErrLeaseUnavailable)
	}
	if reservation.LeaseToken != "" && reservation.LeaseUntil.After(now) {
		return nil, ErrLeaseUnavailable
	}
	if owner == "" || leaseToken == "" || !leaseUntil.After(now) {
		return nil, ErrLeaseUnavailable
	}

	reservation.LeaseOwner = owner
	reservation.LeaseToken = leaseToken
	reservation.LeaseUntil = leaseUntil
	reservation.LastHeartbeatAt = now
	reservation.UpdatedAt = now
	s.storeReservationLocked(reservation)
	cloned := cloneReservation(reservation)
	return &cloned, nil
}

func (s *MemoryStore) BeginProvingOperation(_ context.Context, operationID string, refs []SubmittedReservationRef, leaseUntil time.Time, now time.Time) ([]NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()

	operation, ok := s.operations[operationID]
	if !ok {
		return nil, nil, ErrOperationNotFound
	}
	if operation.Status != OperationStatusPlanned {
		return nil, nil, fmt.Errorf("%w: operation %s expected Planned got %s", ErrCompareAndSetFailed, operationID, operation.Status)
	}
	if len(refs) == 0 || !leaseUntil.After(now) {
		return nil, nil, fmt.Errorf("%w: valid reservation leases are required", ErrLeaseUnavailable)
	}

	reservations := make([]NoteReservation, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref.ReservationID == "" || strings.TrimSpace(ref.LeaseOwner) == "" || ref.LeaseToken == "" {
			return nil, nil, fmt.Errorf("%w: reservation id, lease owner, and lease token are required", ErrLeaseUnavailable)
		}
		if _, exists := seen[ref.ReservationID]; exists {
			return nil, nil, fmt.Errorf("%w: duplicate reservation_id %s", ErrInvalidReservation, ref.ReservationID)
		}
		seen[ref.ReservationID] = struct{}{}
		reservation, exists := s.reservations[ref.ReservationID]
		if !exists {
			return nil, nil, ErrReservationNotFound
		}
		if reservation.Status != StatusReserved {
			return nil, nil, fmt.Errorf("%w: expected Reserved got %s", ErrCompareAndSetFailed, reservation.Status)
		}
		if reservation.OperationID != operationID {
			return nil, nil, fmt.Errorf("%w: reservation %s belongs to operation %s", ErrInvalidReservation, reservation.ReservationID, reservation.OperationID)
		}
		if reservation.LeaseToken != "" && reservation.LeaseUntil.After(now) {
			return nil, nil, ErrLeaseUnavailable
		}
		if reservation.RelayHandedOff {
			return nil, nil, fmt.Errorf("%w: relay payload was handed off", ErrInvalidReservation)
		}
		reservation.LeaseOwner = strings.TrimSpace(ref.LeaseOwner)
		reservation.LeaseToken = ref.LeaseToken
		reservation.LeaseUntil = leaseUntil
		reservation.LastHeartbeatAt = now
		reservation.Status = StatusProving
		reservation.UpdatedAt = now
		reservations = append(reservations, reservation)
	}
	if err := s.validateReservationOperationLinksLocked(reservations, map[string]struct{}{operationID: {}}); err != nil {
		return nil, nil, err
	}

	operation.Status = OperationStatusProving
	operation.UpdatedAt = now
	s.operations[operationID] = operation
	updated := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		s.storeReservationLocked(reservation)
		updated = append(updated, cloneReservation(reservation))
	}
	clonedOperation := cloneOperation(operation)
	return updated, &clonedOperation, nil
}

func (s *MemoryStore) RollbackProvingOperation(_ context.Context, operationID string, refs []SubmittedReservationRef, now time.Time) ([]NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()

	operation, ok := s.operations[operationID]
	if !ok {
		return nil, nil, ErrOperationNotFound
	}
	if operation.Status != OperationStatusProving {
		return nil, nil, fmt.Errorf("%w: operation %s expected Proving got %s", ErrCompareAndSetFailed, operationID, operation.Status)
	}
	reservations, err := s.validateLeasedReservationsForStatusLocked(refs, StatusProving, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateReservationOperationLinksLocked(reservations, map[string]struct{}{operationID: {}}); err != nil {
		return nil, nil, err
	}

	operation.Status = OperationStatusPlanned
	operation.UpdatedAt = now
	s.operations[operationID] = operation
	updated := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.Status = StatusReserved
		clearReservationLeaseFields(&reservation)
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
		updated = append(updated, cloneReservation(reservation))
	}
	clonedOperation := cloneOperation(operation)
	return updated, &clonedOperation, nil
}

func (s *MemoryStore) HeartbeatReservationLease(_ context.Context, reservationID string, leaseOwner string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if err := requireLeaseToken(reservation, leaseOwner, leaseToken, now); err != nil {
		return nil, err
	}
	if !leaseUntil.After(now) {
		return nil, ErrLeaseUnavailable
	}

	reservation.LeaseUntil = leaseUntil
	reservation.LastHeartbeatAt = now
	reservation.UpdatedAt = now
	s.storeReservationLocked(reservation)
	cloned := cloneReservation(reservation)
	return &cloned, nil
}

func (s *MemoryStore) HeartbeatReservationLeaseForStatus(_ context.Context, reservationID string, leaseOwner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if reservation.Status != requiredStatus {
		return nil, fmt.Errorf("%w: expected %s got %s", ErrCompareAndSetFailed, requiredStatus, reservation.Status)
	}
	if err := requireLeaseToken(reservation, leaseOwner, leaseToken, now); err != nil {
		return nil, err
	}
	if !leaseUntil.After(now) {
		return nil, ErrLeaseUnavailable
	}

	reservation.LeaseUntil = leaseUntil
	reservation.LastHeartbeatAt = now
	reservation.UpdatedAt = now
	s.storeReservationLocked(reservation)
	cloned := cloneReservation(reservation)
	return &cloned, nil
}

func (s *MemoryStore) RecordRelayHandoff(_ context.Context, reservationID string, leaseOwner string, leaseToken string, payloadHash string, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	updated, err := s.recordRelayHandoffBatchLocked(reservation.OperationID, []SubmittedReservationRef{{
		ReservationID: reservationID,
		LeaseOwner:    leaseOwner,
		LeaseToken:    leaseToken,
	}}, payloadHash, now)
	if err != nil {
		return nil, err
	}
	return &updated[0], nil
}

// RecordRelayHandoffBatch durably binds one relay payload to the complete
// ProofReady reservation set of an operation. Partial handoffs are unsafe:
// another worker could otherwise continue with an unmarked input.
func (s *MemoryStore) RecordRelayHandoffBatch(_ context.Context, operationID string, refs []SubmittedReservationRef, payloadHash string, now time.Time) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordRelayHandoffBatchLocked(operationID, refs, payloadHash, now)
}

func (s *MemoryStore) recordRelayHandoffBatchLocked(operationID string, refs []SubmittedReservationRef, payloadHash string, now time.Time) ([]NoteReservation, error) {
	if len(refs) == 0 {
		return nil, fmt.Errorf("%w: relay handoff requires reservation refs", ErrInvalidReservation)
	}
	if payloadHash == "" {
		return nil, fmt.Errorf("%w: relay handoff payload hash is required", ErrInvalidReservation)
	}
	refByID := make(map[string]SubmittedReservationRef, len(refs))
	for _, ref := range refs {
		if ref.ReservationID == "" {
			return nil, fmt.Errorf("%w: relay handoff reservation_id is required", ErrInvalidReservation)
		}
		if _, duplicate := refByID[ref.ReservationID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate relay handoff reservation %s", ErrInvalidReservation, ref.ReservationID)
		}
		refByID[ref.ReservationID] = ref
	}

	expectedIDs := make(map[string]struct{}, len(refs))
	if operationID != "" {
		operation, ok := s.operations[operationID]
		if !ok {
			return nil, ErrOperationNotFound
		}
		if operation.Status != OperationStatusProofReady || operation.PayloadHash == "" || operation.PayloadHash != payloadHash {
			return nil, fmt.Errorf("%w: relay handoff payload hash does not match ProofReady operation", ErrInvalidReservation)
		}
		for reservationID, reservation := range s.reservations {
			if reservation.OperationID == operationID {
				expectedIDs[reservationID] = struct{}{}
			}
		}
		if len(expectedIDs) == 0 {
			return nil, fmt.Errorf("%w: relay handoff operation has no reservations", ErrInvalidReservation)
		}
	} else {
		if len(refs) != 1 {
			return nil, fmt.Errorf("%w: unlinked relay handoff accepts exactly one reservation", ErrInvalidReservation)
		}
		reservation, ok := s.reservations[refs[0].ReservationID]
		if !ok {
			return nil, ErrReservationNotFound
		}
		if reservation.OperationID != "" {
			return nil, fmt.Errorf("%w: linked relay handoff requires operation_id %s", ErrInvalidReservation, reservation.OperationID)
		}
		expectedIDs[refs[0].ReservationID] = struct{}{}
	}
	if len(refByID) != len(expectedIDs) {
		return nil, fmt.Errorf("%w: relay handoff reservation set does not match operation", ErrInvalidReservation)
	}
	for reservationID := range expectedIDs {
		if _, included := refByID[reservationID]; !included {
			return nil, fmt.Errorf("%w: relay handoff is missing operation reservation %s", ErrInvalidReservation, reservationID)
		}
	}

	for _, ref := range refs {
		reservation, ok := s.reservations[ref.ReservationID]
		if !ok {
			return nil, ErrReservationNotFound
		}
		if reservation.Status != StatusProofReady {
			return nil, fmt.Errorf("%w: relay handoff requires ProofReady, got %s", ErrInvalidTransition, reservation.Status)
		}
		if err := requireLeaseToken(reservation, ref.LeaseOwner, ref.LeaseToken, now); err != nil {
			return nil, err
		}
		if reservation.PayloadHash == "" || reservation.PayloadHash != payloadHash {
			return nil, fmt.Errorf("%w: relay handoff payload hash does not match ProofReady reservation", ErrInvalidReservation)
		}
	}

	updated := make([]NoteReservation, 0, len(refs))
	for _, ref := range refs {
		reservation := s.reservations[ref.ReservationID]
		if !reservation.RelayHandedOff {
			reservation.RelayHandedOff = true
			reservation.RelayHandedOffAt = now
			reservation.UpdatedAt = now
			s.storeReservationLocked(reservation)
		}
		updated = append(updated, cloneReservation(reservation))
	}
	return updated, nil
}

func (s *MemoryStore) ClearReservationLease(_ context.Context, reservationID string, leaseOwner string, leaseToken string, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if err := requireLeaseToken(reservation, leaseOwner, leaseToken, now); err != nil {
		return nil, err
	}
	if reservation.RelayHandedOff {
		return nil, fmt.Errorf("%w: relay payload was handed off", ErrInvalidReservation)
	}
	if reservation.Status != StatusReserved {
		return nil, fmt.Errorf("%w: lease clearing is only allowed for Reserved rollback", ErrInvalidTransition)
	}

	reservation.LeaseOwner = ""
	reservation.LeaseToken = ""
	reservation.LeaseUntil = time.Time{}
	reservation.UpdatedAt = now
	s.storeReservationLocked(reservation)
	cloned := cloneReservation(reservation)
	return &cloned, nil
}

func (s *MemoryStore) MarkReservationsProofReady(_ context.Context, refs []SubmittedReservationRef, operationUpdate ProofReadyOperationUpdate, now time.Time) ([]NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(refs) == 0 && operationUpdate.OperationID != "" {
		return nil, nil, fmt.Errorf("%w: reservation refs are required", ErrInvalidReservation)
	}
	reservations, err := s.validateLeasedReservationsForStatusLocked(refs, StatusProving, now)
	if err != nil {
		return nil, nil, err
	}
	if operationUpdate.OperationID == "" {
		for _, reservation := range reservations {
			if reservation.OperationID != "" {
				return nil, nil, fmt.Errorf("%w: operation_id is required for linked reservation %s", ErrInvalidReservation, reservation.ReservationID)
			}
		}
		if strings.TrimSpace(operationUpdate.PayloadHash) == "" {
			return nil, nil, fmt.Errorf("%w: payload_hash is required for ProofReady", ErrInvalidReservation)
		}
	}

	var operation *PayrollOperation
	if operationUpdate.OperationID != "" {
		existing, ok := s.operations[operationUpdate.OperationID]
		if !ok {
			return nil, nil, ErrOperationNotFound
		}
		if err := validateOperationStatusAdvance(existing, OperationStatusProofReady); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(operationUpdate.PayloadHash) == "" {
			return nil, nil, fmt.Errorf("%w: payload_hash is required for ProofReady", ErrInvalidReservation)
		}
		if err := s.validateReservationOperationLinksLocked(reservations, map[string]struct{}{operationUpdate.OperationID: {}}); err != nil {
			return nil, nil, err
		}
		if updated, err := mergeExpectedProofReadyValue("expected_output_commitment", existing.ExpectedOutputCommitment, operationUpdate.ExpectedOutputCommitment); err != nil {
			return nil, nil, err
		} else {
			existing.ExpectedOutputCommitment = updated
		}
		if updated, err := mergeExpectedProofReadyValue("expected_disclosure_digest", existing.ExpectedDisclosureDigest, operationUpdate.ExpectedDisclosureDigest); err != nil {
			return nil, nil, err
		} else {
			existing.ExpectedDisclosureDigest = updated
		}
		if updated, err := mergeExpectedProofReadyValue("expected_user_disclosure_digest", existing.ExpectedUserDisclosureDigest, operationUpdate.ExpectedUserDisclosureDigest); err != nil {
			return nil, nil, err
		} else {
			existing.ExpectedUserDisclosureDigest = updated
		}
		if updated, err := mergeExpectedProofReadyValue("expected_audit_disclosure_digest", existing.ExpectedAuditDisclosureDigest, operationUpdate.ExpectedAuditDisclosureDigest); err != nil {
			return nil, nil, err
		} else {
			existing.ExpectedAuditDisclosureDigest = updated
		}
		if updated, err := mergeExpectedProofReadyValue("expected_self_view_disclosure_digest", existing.ExpectedSelfViewDisclosureDigest, operationUpdate.ExpectedSelfViewDisclosureDigest); err != nil {
			return nil, nil, err
		} else {
			existing.ExpectedSelfViewDisclosureDigest = updated
		}
		if updated, err := mergeExpectedProofReadyValue("payload_hash", existing.PayloadHash, operationUpdate.PayloadHash); err != nil {
			return nil, nil, err
		} else {
			existing.PayloadHash = updated
		}
		if updated, err := mergeExpectedProofReadyValue("sign_doc_hash", existing.SignDocHash, operationUpdate.SignDocHash); err != nil {
			return nil, nil, err
		} else {
			existing.SignDocHash = updated
		}
		if updated, err := mergeExpectedProofReadyValue("tx_bytes_hash", existing.TxBytesHash, operationUpdate.TxBytesHash); err != nil {
			return nil, nil, err
		} else {
			existing.TxBytesHash = updated
		}
		existing.Status = OperationStatusProofReady
		existing.UpdatedAt = now
		operation = &existing
	}

	updatedReservations := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		payloadHash := operationUpdate.PayloadHash
		if operation != nil && operation.PayloadHash != "" {
			payloadHash = operation.PayloadHash
		}
		if updated, err := mergeExpectedProofReadyValue("payload_hash", reservation.PayloadHash, payloadHash); err != nil {
			return nil, nil, err
		} else {
			reservation.PayloadHash = updated
		}
		if updated, err := mergeExpectedProofReadyValue("sign_doc_hash", reservation.SignDocHash, operationUpdate.SignDocHash); err != nil {
			return nil, nil, err
		} else {
			reservation.SignDocHash = updated
		}
		if updated, err := mergeExpectedProofReadyValue("tx_bytes_hash", reservation.TxBytesHash, operationUpdate.TxBytesHash); err != nil {
			return nil, nil, err
		} else {
			reservation.TxBytesHash = updated
		}
		reservation.Status = StatusProofReady
		reservation.UpdatedAt = now
		updatedReservations = append(updatedReservations, cloneReservation(reservation))
	}

	if operation != nil {
		s.operations[operation.OperationID] = *operation
		cloned := cloneOperation(*operation)
		operation = &cloned
	}
	for _, reservation := range updatedReservations {
		s.storeReservationLocked(reservation)
	}

	return updatedReservations, operation, nil
}

func (s *MemoryStore) MarkReservationsProofDiscarding(_ context.Context, operationID string, refs []SubmittedReservationRef, now time.Time) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(refs) == 0 {
		return nil, fmt.Errorf("%w: reservation refs are required", ErrInvalidReservation)
	}
	reservations, err := s.validateLeasedReservationsForStatusLocked(refs, StatusProofReady, now)
	if err != nil {
		return nil, err
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		for _, reservation := range reservations {
			if reservation.OperationID != "" {
				return nil, fmt.Errorf("%w: operation_id is required for linked reservation %s", ErrInvalidReservation, reservation.ReservationID)
			}
		}
	} else {
		if _, ok := s.operations[operationID]; !ok {
			return nil, ErrOperationNotFound
		}
		if err := s.validateReservationOperationLinksLocked(reservations, map[string]struct{}{operationID: {}}); err != nil {
			return nil, err
		}
	}

	updated := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		if reservation.RelayHandedOff || reservation.BroadcastInFlight || reservation.BroadcastAttemptCount != 0 {
			return nil, fmt.Errorf("%w: reservation has handoff or broadcast attempt evidence", ErrInvalidReservation)
		}
		reservation.ProofDiscardInFlight = true
		if reservation.ProofDiscardStartedAt.IsZero() {
			reservation.ProofDiscardStartedAt = now
		}
		reservation.UpdatedAt = now
		updated = append(updated, reservation)
	}
	for _, reservation := range updated {
		s.storeReservationLocked(reservation)
	}
	cloned := make([]NoteReservation, 0, len(updated))
	for _, reservation := range updated {
		cloned = append(cloned, cloneReservation(reservation))
	}
	return cloned, nil
}

func (s *MemoryStore) MarkReservationSubmitted(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, update SubmittedReservationUpdate, now time.Time) (*NoteReservation, error) {
	updated, _, err := s.MarkReservationsSubmitted(ctx, []SubmittedReservationRef{{
		ReservationID: reservationID,
		LeaseOwner:    leaseOwner,
		LeaseToken:    leaseToken,
	}}, nil, update, now)
	if err != nil {
		return nil, err
	}
	return &updated[0], nil
}

func (s *MemoryStore) MarkReservationsBroadcastAttempting(_ context.Context, refs []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptStart, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(refs) == 0 {
		return nil, nil, fmt.Errorf("%w: reservation refs are required", ErrInvalidReservation)
	}
	reservations, err := s.validateLeasedReservationsForStatusLocked(refs, StatusProofReady, now)
	if err != nil {
		return nil, nil, err
	}
	effectiveOperationIDs := operationIDsForReservationUpdate(reservations, operationIDs)
	operations, operationIDSet, err := s.loadLinkedOperationsLocked(effectiveOperationIDs)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateReservationOperationLinksLocked(reservations, operationIDSet); err != nil {
		return nil, nil, err
	}
	for _, operation := range operations {
		if operation.Status != OperationStatusProofReady {
			return nil, nil, fmt.Errorf("%w: operation %s is not ProofReady", ErrCompareAndSetFailed, operation.OperationID)
		}
	}
	for _, reservation := range reservations {
		if reservation.RelayHandedOff {
			return nil, nil, fmt.Errorf("%w: relay payload was handed off", ErrInvalidReservation)
		}
		if reservation.ProofDiscardInFlight {
			return nil, nil, fmt.Errorf("%w: proof discard is in progress", ErrCompareAndSetFailed)
		}
		if reservation.BroadcastInFlight || reservation.BroadcastAttemptCount != 0 {
			return nil, nil, fmt.Errorf("%w: broadcast attempt already started; reconcile before retry", ErrCompareAndSetFailed)
		}
	}
	if err := validateBroadcastIdentityPreflight(reservations, operations, update.TxHash, update.TxBytesHash, update.SignDocHash); err != nil {
		return nil, nil, err
	}

	reason := strings.TrimSpace(update.Reason)
	if reason == "" {
		reason = "broadcast attempt started"
	}
	updated := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.BroadcastAttemptCount++
		reservation.BroadcastInFlight = true
		reservation.TxHash, _ = mergeExpectedProofReadyValue("tx_hash", reservation.TxHash, update.TxHash)
		reservation.TxBytesHash, _ = mergeExpectedProofReadyValue("tx_bytes_hash", reservation.TxBytesHash, update.TxBytesHash)
		reservation.SignDocHash, _ = mergeExpectedProofReadyValue("sign_doc_hash", reservation.SignDocHash, update.SignDocHash)
		reservation.AccountSequence = update.AccountSequence
		reservation.LastBroadcastAt = now
		reservation.LastBroadcastError = reason
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
		updated = append(updated, cloneReservation(reservation))
	}
	clonedOperations := make([]PayrollOperation, 0, len(operations))
	for _, operation := range operations {
		operation.TxHash, _ = mergeExpectedProofReadyValue("tx_hash", operation.TxHash, update.TxHash)
		operation.TxBytesHash, _ = mergeExpectedProofReadyValue("tx_bytes_hash", operation.TxBytesHash, update.TxBytesHash)
		operation.SignDocHash, _ = mergeExpectedProofReadyValue("sign_doc_hash", operation.SignDocHash, update.SignDocHash)
		operation.UpdatedAt = now
		s.operations[operation.OperationID] = operation
		clonedOperations = append(clonedOperations, cloneOperation(operation))
	}
	return updated, clonedOperations, nil
}

func (s *MemoryStore) MarkReservationsSubmitted(_ context.Context, refs []SubmittedReservationRef, operationIDs []string, update SubmittedReservationUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(refs) == 0 {
		if hasOperationID(operationIDs) {
			return nil, nil, fmt.Errorf("%w: reservation refs are required", ErrInvalidReservation)
		}
		return []NoteReservation{}, []PayrollOperation{}, nil
	}
	if !submittedUpdateHasTxIdentity(update) {
		return nil, nil, fmt.Errorf("%w: submitted update requires tx_hash or tx_bytes_hash", ErrInvalidReservation)
	}

	reservations, err := s.validateLeasedReservationsForStatusLocked(refs, StatusProofReady, now)
	if err != nil {
		return nil, nil, err
	}
	if err := requireDurableBroadcastAttempt(reservations); err != nil {
		return nil, nil, err
	}

	effectiveOperationIDs := operationIDsForReservationUpdate(reservations, operationIDs)
	operations, operationIDSet, err := s.loadLinkedOperationsLocked(effectiveOperationIDs)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateReservationOperationLinksLocked(reservations, operationIDSet); err != nil {
		return nil, nil, err
	}
	if err := validateOperationStatusBatchAdvance(operations, OperationStatusSubmitted); err != nil {
		return nil, nil, err
	}
	if err := validateBroadcastIdentityPreflight(reservations, operations, update.TxHash, update.TxBytesHash, update.SignDocHash); err != nil {
		return nil, nil, err
	}

	updatedReservations := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.Status = StatusSubmitted
		reservation.TxHash, _ = mergeExpectedProofReadyValue("tx_hash", reservation.TxHash, update.TxHash)
		reservation.TxBytesHash, _ = mergeExpectedProofReadyValue("tx_bytes_hash", reservation.TxBytesHash, update.TxBytesHash)
		reservation.SignDocHash, _ = mergeExpectedProofReadyValue("sign_doc_hash", reservation.SignDocHash, update.SignDocHash)
		reservation.AccountSequence = update.AccountSequence
		reservation.BroadcastInFlight = false
		reservation.LastBroadcastAt = now
		reservation.LastBroadcastError = update.LastBroadcastError
		clearReservationLeaseFields(&reservation)
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
		updatedReservations = append(updatedReservations, cloneReservation(reservation))
	}

	updatedOperations := make([]PayrollOperation, 0, len(operations))
	for _, operation := range operations {
		operation.Status = OperationStatusSubmitted
		operation.TxHash, _ = mergeExpectedProofReadyValue("tx_hash", operation.TxHash, update.TxHash)
		operation.TxBytesHash, _ = mergeExpectedProofReadyValue("tx_bytes_hash", operation.TxBytesHash, update.TxBytesHash)
		operation.SignDocHash, _ = mergeExpectedProofReadyValue("sign_doc_hash", operation.SignDocHash, update.SignDocHash)
		operation.UpdatedAt = now
		s.operations[operation.OperationID] = operation
		updatedOperations = append(updatedOperations, cloneOperation(operation))
	}

	return updatedReservations, updatedOperations, nil
}

func (s *MemoryStore) MarkReservationsBroadcastUnknown(_ context.Context, refs []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(refs) == 0 {
		if hasOperationID(operationIDs) {
			return nil, nil, fmt.Errorf("%w: reservation refs are required", ErrInvalidReservation)
		}
		return []NoteReservation{}, []PayrollOperation{}, nil
	}
	if !broadcastAttemptHasTxIdentity(update) {
		return nil, nil, fmt.Errorf("%w: broadcast attempt update requires tx_hash, tx_bytes_hash, or sign_doc_hash", ErrInvalidReservation)
	}

	reservations, err := s.validateLeasedReservationsForStatusLocked(refs, StatusProofReady, now)
	if err != nil {
		return nil, nil, err
	}
	if err := requireDurableBroadcastAttempt(reservations); err != nil {
		return nil, nil, err
	}
	effectiveOperationIDs := operationIDsForReservationUpdate(reservations, operationIDs)
	operations, operationIDSet, err := s.loadLinkedOperationsLocked(effectiveOperationIDs)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateReservationOperationLinksLocked(reservations, operationIDSet); err != nil {
		return nil, nil, err
	}
	if err := validateOperationStatusBatchAdvance(operations, OperationStatusUnknown); err != nil {
		return nil, nil, err
	}
	if err := validateBroadcastIdentityPreflight(reservations, operations, update.TxHash, update.TxBytesHash, update.SignDocHash); err != nil {
		return nil, nil, err
	}

	updatedReservations := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.Status = StatusUnknown
		reservation.TxHash, _ = mergeExpectedProofReadyValue("tx_hash", reservation.TxHash, update.TxHash)
		reservation.TxBytesHash, _ = mergeExpectedProofReadyValue("tx_bytes_hash", reservation.TxBytesHash, update.TxBytesHash)
		reservation.SignDocHash, _ = mergeExpectedProofReadyValue("sign_doc_hash", reservation.SignDocHash, update.SignDocHash)
		reservation.AccountSequence = update.AccountSequence
		reservation.BroadcastInFlight = false
		reservation.LastBroadcastAt = now
		reservation.LastBroadcastError = update.LastBroadcastError
		clearReservationLeaseFields(&reservation)
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
		updatedReservations = append(updatedReservations, cloneReservation(reservation))
	}

	updatedOperations := make([]PayrollOperation, 0, len(operations))
	for _, operation := range operations {
		operation.Status = OperationStatusUnknown
		operation.TxHash, _ = mergeExpectedProofReadyValue("tx_hash", operation.TxHash, update.TxHash)
		operation.TxBytesHash, _ = mergeExpectedProofReadyValue("tx_bytes_hash", operation.TxBytesHash, update.TxBytesHash)
		operation.SignDocHash, _ = mergeExpectedProofReadyValue("sign_doc_hash", operation.SignDocHash, update.SignDocHash)
		operation.UpdatedAt = now
		s.operations[operation.OperationID] = operation
		updatedOperations = append(updatedOperations, cloneOperation(operation))
	}

	return updatedReservations, updatedOperations, nil
}

func (s *MemoryStore) MarkReservationsBroadcastAmbiguous(_ context.Context, refs []SubmittedReservationRef, operationIDs []string, update BroadcastAmbiguityUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(refs) == 0 {
		if hasOperationID(operationIDs) {
			return nil, nil, fmt.Errorf("%w: reservation refs are required", ErrInvalidReservation)
		}
		return []NoteReservation{}, []PayrollOperation{}, nil
	}

	reservations, err := s.validateLeasedReservationsForStatusLocked(refs, StatusProofReady, now)
	if err != nil {
		return nil, nil, err
	}
	if err := requireDurableBroadcastAttempt(reservations); err != nil {
		return nil, nil, err
	}
	effectiveOperationIDs := operationIDsForReservationUpdate(reservations, operationIDs)
	operations, operationIDSet, err := s.loadLinkedOperationsLocked(effectiveOperationIDs)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateReservationOperationLinksLocked(reservations, operationIDSet); err != nil {
		return nil, nil, err
	}
	if err := validateOperationStatusBatchAdvance(operations, OperationStatusManualReview); err != nil {
		return nil, nil, err
	}

	updatedReservations := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.Status = StatusManualReview
		reservation.BroadcastInFlight = false
		reservation.LastBroadcastAt = now
		reservation.LastBroadcastError = update.LastBroadcastError
		clearReservationLeaseFields(&reservation)
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
		updatedReservations = append(updatedReservations, cloneReservation(reservation))
	}

	updatedOperations := make([]PayrollOperation, 0, len(operations))
	for _, operation := range operations {
		operation.Status = OperationStatusManualReview
		operation.UpdatedAt = now
		s.operations[operation.OperationID] = operation
		updatedOperations = append(updatedOperations, cloneOperation(operation))
	}

	return updatedReservations, updatedOperations, nil
}

func (s *MemoryStore) MarkReservationsProofArtifactCleanupFailed(_ context.Context, refs []SubmittedReservationRef, operationIDs []string, reason string, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(refs) == 0 {
		return nil, nil, fmt.Errorf("%w: proving reservation refs are required", ErrInvalidReservation)
	}
	reservations, err := s.validateLeasedReservationsForStatusLocked(refs, StatusProving, now)
	if err != nil {
		return nil, nil, err
	}
	effectiveOperationIDs := operationIDsForReservationUpdate(reservations, operationIDs)
	operations, operationIDSet, err := s.loadLinkedOperationsLocked(effectiveOperationIDs)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateReservationOperationLinksLocked(reservations, operationIDSet); err != nil {
		return nil, nil, err
	}
	if err := validateOperationStatusBatchAdvance(operations, OperationStatusManualReview); err != nil {
		return nil, nil, err
	}

	reviewReason := strings.TrimSpace(reason)
	if reviewReason == "" {
		reviewReason = "proof artifact cleanup could not be confirmed"
	}
	updatedReservations := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.Status = StatusManualReview
		reservation.ReconciliationReviewReason = reviewReason
		reservation.LastReconciledAt = now
		clearReservationLeaseFields(&reservation)
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
		updatedReservations = append(updatedReservations, cloneReservation(reservation))
	}
	updatedOperations := make([]PayrollOperation, 0, len(operations))
	for _, operation := range operations {
		operation.Status = OperationStatusManualReview
		operation.UpdatedAt = now
		s.operations[operation.OperationID] = operation
		updatedOperations = append(updatedOperations, cloneOperation(operation))
	}
	return updatedReservations, updatedOperations, nil
}

func (s *MemoryStore) validateLeasedReservationsForStatusLocked(refs []SubmittedReservationRef, status ReservationStatus, now time.Time) ([]NoteReservation, error) {
	if len(refs) == 0 {
		return []NoteReservation{}, nil
	}

	reservations := make([]NoteReservation, 0, len(refs))
	seenReservations := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref.ReservationID == "" {
			return nil, fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
		}
		if _, exists := seenReservations[ref.ReservationID]; exists {
			return nil, fmt.Errorf("%w: duplicate reservation_id %s", ErrInvalidReservation, ref.ReservationID)
		}
		seenReservations[ref.ReservationID] = struct{}{}

		reservation, ok := s.reservations[ref.ReservationID]
		if !ok {
			return nil, ErrReservationNotFound
		}
		if reservation.Status != status {
			return nil, fmt.Errorf("%w: expected %s got %s", ErrCompareAndSetFailed, status, reservation.Status)
		}
		if err := requireLeaseToken(reservation, ref.LeaseOwner, ref.LeaseToken, now); err != nil {
			return nil, err
		}
		reservations = append(reservations, reservation)
	}
	return reservations, nil
}

func hasOperationID(operationIDs []string) bool {
	for _, operationID := range operationIDs {
		if operationID != "" {
			return true
		}
	}
	return false
}

func operationIDsForReservationUpdate(reservations []NoteReservation, operationIDs []string) []string {
	if hasOperationID(operationIDs) {
		return operationIDs
	}
	linkedOperationIDs := make([]string, 0, len(reservations))
	for _, reservation := range reservations {
		if reservation.OperationID != "" {
			linkedOperationIDs = append(linkedOperationIDs, reservation.OperationID)
		}
	}
	return linkedOperationIDs
}

func submittedUpdateHasTxIdentity(update SubmittedReservationUpdate) bool {
	return strings.TrimSpace(update.TxHash) != "" ||
		strings.TrimSpace(update.TxBytesHash) != ""
}

func broadcastAttemptHasTxIdentity(update BroadcastAttemptUpdate) bool {
	return strings.TrimSpace(update.TxHash) != "" ||
		strings.TrimSpace(update.TxBytesHash) != ""
}

func (s *MemoryStore) loadLinkedOperationsLocked(operationIDs []string) ([]PayrollOperation, map[string]struct{}, error) {
	operations := make([]PayrollOperation, 0, len(operationIDs))
	seenOperations := make(map[string]struct{}, len(operationIDs))
	operationIDSet := make(map[string]struct{}, len(operationIDs))
	for _, operationID := range operationIDs {
		if operationID == "" {
			continue
		}
		if _, exists := seenOperations[operationID]; exists {
			continue
		}
		seenOperations[operationID] = struct{}{}
		operationIDSet[operationID] = struct{}{}

		operation, ok := s.operations[operationID]
		if !ok {
			return nil, nil, ErrOperationNotFound
		}
		operations = append(operations, operation)
	}
	return operations, operationIDSet, nil
}

func validateOperationStatusAdvance(existing PayrollOperation, next OperationStatus) error {
	if IsTerminalOperationStatus(existing.Status) && existing.Status != next {
		return fmt.Errorf("%w: terminal operation %s cannot be overwritten from %s to %s", ErrCompareAndSetFailed, existing.OperationID, existing.Status, next)
	}
	return nil
}

func validateOperationStatusBatchAdvance(operations []PayrollOperation, target OperationStatus) error {
	for _, operation := range operations {
		if err := validateOperationStatusAdvance(operation, target); err != nil {
			return err
		}
	}
	return nil
}

func (s *MemoryStore) validateReservationOperationLinksLocked(reservations []NoteReservation, operationIDs map[string]struct{}) error {
	if len(operationIDs) == 0 {
		return nil
	}
	linked := make(map[string]map[string]struct{}, len(operationIDs))
	for _, reservation := range reservations {
		if reservation.OperationID == "" {
			return fmt.Errorf("%w: reservation %s has no operation_id", ErrInvalidReservation, reservation.ReservationID)
		}
		if _, ok := operationIDs[reservation.OperationID]; !ok {
			return fmt.Errorf("%w: reservation %s belongs to operation %s", ErrInvalidReservation, reservation.ReservationID, reservation.OperationID)
		}
		if linked[reservation.OperationID] == nil {
			linked[reservation.OperationID] = make(map[string]struct{})
		}
		linked[reservation.OperationID][reservation.ReservationID] = struct{}{}
	}
	for operationID := range operationIDs {
		if _, ok := linked[operationID]; !ok {
			return fmt.Errorf("%w: operation %s has no linked reservation", ErrInvalidReservation, operationID)
		}
	}
	for _, reservation := range s.reservations {
		if reservation.OperationID == "" {
			continue
		}
		if _, ok := operationIDs[reservation.OperationID]; !ok {
			continue
		}
		if _, ok := linked[reservation.OperationID][reservation.ReservationID]; !ok {
			return fmt.Errorf("%w: operation %s missing reservation %s", ErrInvalidReservation, reservation.OperationID, reservation.ReservationID)
		}
	}
	return nil
}

func clearLeaseForStatusTransition(reservation *NoteReservation, to ReservationStatus) {
	switch to {
	case StatusProving, StatusProofReady:
		return
	default:
		reservation.BroadcastInFlight = false
		reservation.ProofDiscardInFlight = false
		reservation.ProofDiscardStartedAt = time.Time{}
		clearReservationLeaseFields(reservation)
	}
}

func clearReservationLeaseFields(reservation *NoteReservation) {
	reservation.LeaseOwner = ""
	reservation.LeaseToken = ""
	reservation.LeaseUntil = time.Time{}
	reservation.LastHeartbeatAt = time.Time{}
}

func mergeExpectedProofReadyValue(fieldName string, existing string, incoming string) (string, error) {
	if incoming == "" {
		return existing, nil
	}
	if existing == "" {
		return incoming, nil
	}
	if normalizeTransactionIdentity(existing) != normalizeTransactionIdentity(incoming) {
		return "", fmt.Errorf("%w: %s mismatch", ErrInvalidReservation, fieldName)
	}
	return existing, nil
}

func normalizeTransactionIdentity(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
}

func requireDurableBroadcastAttempt(reservations []NoteReservation) error {
	for _, reservation := range reservations {
		if !reservation.BroadcastInFlight || reservation.BroadcastAttemptCount < 1 {
			return fmt.Errorf("%w: durable broadcast attempt is required before terminal bookkeeping", ErrCompareAndSetFailed)
		}
	}
	return nil
}

func validateBroadcastIdentityPreflight(reservations []NoteReservation, operations []PayrollOperation, txHash string, txBytesHash string, signDocHash string) error {
	for _, reservation := range reservations {
		if _, err := mergeExpectedProofReadyValue("tx_hash", reservation.TxHash, txHash); err != nil {
			return err
		}
		if _, err := mergeExpectedProofReadyValue("tx_bytes_hash", reservation.TxBytesHash, txBytesHash); err != nil {
			return err
		}
		if _, err := mergeExpectedProofReadyValue("sign_doc_hash", reservation.SignDocHash, signDocHash); err != nil {
			return err
		}
	}
	for _, operation := range operations {
		if _, err := mergeExpectedProofReadyValue("tx_hash", operation.TxHash, txHash); err != nil {
			return err
		}
		if _, err := mergeExpectedProofReadyValue("tx_bytes_hash", operation.TxBytesHash, txBytesHash); err != nil {
			return err
		}
		if _, err := mergeExpectedProofReadyValue("sign_doc_hash", operation.SignDocHash, signDocHash); err != nil {
			return err
		}
	}
	return nil
}

func (s *MemoryStore) CreateOperation(_ context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()

	if err := s.validateOperationCreateLocked(operation, nil); err != nil {
		return nil, err
	}
	if operation.ReservationID != "" {
		reservation, exists := s.reservations[operation.ReservationID]
		if !exists || reservation.OperationID != operation.OperationID {
			return nil, fmt.Errorf("%w: operation %s references missing or mismatched reservation %s", ErrInvalidReservation, operation.OperationID, operation.ReservationID)
		}
	}
	s.operations[operation.OperationID] = cloneOperation(operation)
	created := cloneOperation(operation)
	return &created, nil
}

func (s *MemoryStore) GetOperation(_ context.Context, operationID string) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	operation, ok := s.operations[operationID]
	if !ok {
		return nil, ErrOperationNotFound
	}
	cloned := cloneOperation(operation)
	return &cloned, nil
}

// UpdateOperation is retained for the SQL and durable-file adapters. Normal
// lifecycle code should use the evidence-aware transition methods instead.
func (s *MemoryStore) UpdateOperation(_ context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()
	if _, ok := s.operations[operation.OperationID]; !ok {
		return nil, ErrOperationNotFound
	}
	s.operations[operation.OperationID] = cloneOperation(operation)
	updated := cloneOperation(operation)
	return &updated, nil
}

func (s *MemoryStore) activeReservationConflictLocked(candidate NoteReservation, excludeReservationID string) bool {
	s.ensureMapsLocked()
	existingID, ok := s.activeReservationByKey[candidate.ActiveKey()]
	if !ok {
		return false
	}
	return existingID != excludeReservationID
}

func (s *MemoryStore) confirmedSpentReservationExistsLocked(candidate NoteReservation, excludeReservationID string) bool {
	for reservationID, reservation := range s.reservations {
		if reservationID == excludeReservationID {
			continue
		}
		if reservation.Status == StatusConfirmedSpent && reservation.ActiveKey() == candidate.ActiveKey() {
			return true
		}
	}
	return false
}

func (s *MemoryStore) ensureMapsLocked() {
	if s.reservations == nil {
		s.reservations = make(map[string]NoteReservation)
	}
	if s.operations == nil {
		s.operations = make(map[string]PayrollOperation)
	}
	if s.batchOperations == nil {
		s.batchOperations = make(map[string]BatchOperation)
	}
	if s.batchInputs == nil {
		s.batchInputs = make(map[string][]OperationInputReservation)
	}
	if s.batchItems == nil {
		s.batchItems = make(map[string][]PayrollItemOutput)
	}
	if s.batchEvidence == nil {
		s.batchEvidence = make(map[string][]ExpectedOutputEvidence)
	}
	if s.activeReservationByKey != nil {
		return
	}
	s.activeReservationByKey = make(map[string]string, len(s.reservations))
	for reservationID, reservation := range s.reservations {
		if IsActiveReservationStatus(reservation.Status) {
			s.activeReservationByKey[reservation.ActiveKey()] = reservationID
		}
	}
}

func (s *MemoryStore) storeReservationLocked(reservation NoteReservation) {
	s.ensureMapsLocked()
	if existing, ok := s.reservations[reservation.ReservationID]; ok && IsActiveReservationStatus(existing.Status) {
		delete(s.activeReservationByKey, existing.ActiveKey())
	}
	cloned := cloneReservation(reservation)
	s.reservations[reservation.ReservationID] = cloned
	if IsActiveReservationStatus(cloned.Status) {
		s.activeReservationByKey[cloned.ActiveKey()] = cloned.ReservationID
	}
}

func cloneReservation(reservation NoteReservation) NoteReservation {
	reservation.EncryptedNullifier = append([]byte(nil), reservation.EncryptedNullifier...)
	return reservation
}

func cloneOperation(operation PayrollOperation) PayrollOperation {
	operation.EncryptedExpectedRecipient = append([]byte(nil), operation.EncryptedExpectedRecipient...)
	operation.EncryptedExpectedAmount = append([]byte(nil), operation.EncryptedExpectedAmount...)
	return operation
}
