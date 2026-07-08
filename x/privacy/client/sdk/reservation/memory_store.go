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
	activeReservationByKey map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		reservations:           make(map[string]NoteReservation),
		operations:             make(map[string]PayrollOperation),
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
	if reservation.ReservationID == "" {
		return fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
	}
	if _, exists := s.reservations[reservation.ReservationID]; exists {
		return fmt.Errorf("%w: reservation_id already exists", ErrActiveReservationExists)
	}
	if _, exists := pending[reservation.ReservationID]; exists {
		return fmt.Errorf("%w: reservation_id already exists in batch", ErrActiveReservationExists)
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

func (s *MemoryStore) ListReservations(_ context.Context, filter ReservationFilter) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	statuses := make(map[ReservationStatus]struct{}, len(filter.Statuses))
	for _, status := range filter.Statuses {
		statuses[status] = struct{}{}
	}

	out := make([]NoteReservation, 0, len(s.reservations))
	for _, reservation := range s.reservations {
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
	return s.compareAndSetReservationStatusLocked(reservationID, from, to, now)
}

func (s *MemoryStore) CompareAndSetReservationStatusWithOperation(_ context.Context, reservationID string, from ReservationStatus, to ReservationStatus, operation *PayrollOperation, now time.Time) (*NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()

	if RequiresLeaseToken(from, to) {
		return nil, nil, fmt.Errorf("%w: %s -> %s requires lease token", ErrLeaseMismatch, from, to)
	}
	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, nil, ErrReservationNotFound
	}
	if reservation.Status != from {
		return nil, nil, ErrCompareAndSetFailed
	}
	if !CanTransitionReservation(from, to) {
		return nil, nil, ErrInvalidTransition
	}
	candidate := reservation
	candidate.Status = to
	if IsActiveReservationStatus(to) && s.activeReservationConflictLocked(candidate, reservationID) {
		return nil, nil, ErrActiveReservationExists
	}
	clearLeaseForStatusTransition(&candidate, to)

	var operationUpdate *PayrollOperation
	if operation != nil {
		if operation.OperationID == "" {
			return nil, nil, fmt.Errorf("%w: operation_id is required", ErrInvalidReservation)
		}
		existing, ok := s.operations[operation.OperationID]
		if !ok {
			return nil, nil, ErrOperationNotFound
		}
		if reservation.OperationID != "" && reservation.OperationID != operation.OperationID {
			return nil, nil, fmt.Errorf("%w: reservation %s belongs to operation %s", ErrInvalidReservation, reservationID, reservation.OperationID)
		}
		cloned := cloneOperation(*operation)
		cloned.ReservationID = existing.ReservationID
		if existing.ReservationID == "" {
			cloned.ReservationID = reservationID
		}
		operationUpdate = &cloned
	}

	candidate.UpdatedAt = now
	s.storeReservationLocked(candidate)
	if operationUpdate != nil {
		s.operations[operationUpdate.OperationID] = *operationUpdate
	}

	clonedReservation := cloneReservation(candidate)
	var clonedOperation *PayrollOperation
	if operationUpdate != nil {
		cloned := cloneOperation(*operationUpdate)
		clonedOperation = &cloned
	}
	return &clonedReservation, clonedOperation, nil
}

func (s *MemoryStore) CompareAndSetReservationStatusWithLease(_ context.Context, reservationID string, leaseToken string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if err := requireLeaseToken(reservation, leaseToken, now); err != nil {
		return nil, err
	}
	return s.compareAndSetReservationStatusLocked(reservationID, from, to, now)
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
	if IsActiveReservationStatus(to) && s.activeReservationConflictLocked(candidate, reservationID) {
		return nil, ErrActiveReservationExists
	}
	clearLeaseForStatusTransition(&candidate, to)
	candidate.UpdatedAt = now
	s.storeReservationLocked(candidate)
	cloned := cloneReservation(candidate)
	return &cloned, nil
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
	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if requiredStatus != "" && reservation.Status != requiredStatus {
		return nil, fmt.Errorf("%w: expected %s got %s", ErrCompareAndSetFailed, requiredStatus, reservation.Status)
	}
	if !IsActiveReservationStatus(reservation.Status) {
		return nil, fmt.Errorf("%w: status %s is not active", ErrLeaseUnavailable, reservation.Status)
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

func (s *MemoryStore) HeartbeatReservationLease(_ context.Context, reservationID string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if err := requireLeaseToken(reservation, leaseToken, now); err != nil {
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

func (s *MemoryStore) HeartbeatReservationLeaseForStatus(_ context.Context, reservationID string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if reservation.Status != requiredStatus {
		return nil, fmt.Errorf("%w: expected %s got %s", ErrCompareAndSetFailed, requiredStatus, reservation.Status)
	}
	if err := requireLeaseToken(reservation, leaseToken, now); err != nil {
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

func (s *MemoryStore) ClearReservationLease(_ context.Context, reservationID string, leaseToken string, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reservation, ok := s.reservations[reservationID]
	if !ok {
		return nil, ErrReservationNotFound
	}
	if err := requireLeaseToken(reservation, leaseToken, now); err != nil {
		return nil, err
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
	}

	var operation *PayrollOperation
	if operationUpdate.OperationID != "" {
		existing, ok := s.operations[operationUpdate.OperationID]
		if !ok {
			return nil, nil, ErrOperationNotFound
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
		existing.Status = OperationStatusProofReady
		existing.UpdatedAt = now
		operation = &existing
	}

	updatedReservations := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.Status = StatusProofReady
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
		updatedReservations = append(updatedReservations, cloneReservation(reservation))
	}

	if operation != nil {
		s.operations[operation.OperationID] = *operation
		cloned := cloneOperation(*operation)
		operation = &cloned
	}

	return updatedReservations, operation, nil
}

func (s *MemoryStore) MarkReservationSubmitted(ctx context.Context, reservationID string, leaseToken string, update SubmittedReservationUpdate, now time.Time) (*NoteReservation, error) {
	updated, _, err := s.MarkReservationsSubmitted(ctx, []SubmittedReservationRef{{
		ReservationID: reservationID,
		LeaseToken:    leaseToken,
	}}, nil, update, now)
	if err != nil {
		return nil, err
	}
	return &updated[0], nil
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
		return nil, nil, fmt.Errorf("%w: submitted update requires tx_hash, tx_bytes_hash, or sign_doc_hash", ErrInvalidReservation)
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

	updatedReservations := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.Status = StatusSubmitted
		reservation.TxHash = update.TxHash
		reservation.TxBytesHash = update.TxBytesHash
		reservation.SignDocHash = update.SignDocHash
		reservation.AccountSequence = update.AccountSequence
		reservation.BroadcastAttemptCount++
		reservation.LastBroadcastAt = now
		clearReservationLeaseFields(&reservation)
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
		updatedReservations = append(updatedReservations, cloneReservation(reservation))
	}

	updatedOperations := make([]PayrollOperation, 0, len(operations))
	for _, operation := range operations {
		operation.Status = OperationStatusSubmitted
		operation.TxHash = update.TxHash
		operation.TxBytesHash = update.TxBytesHash
		operation.SignDocHash = update.SignDocHash
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
	effectiveOperationIDs := operationIDsForReservationUpdate(reservations, operationIDs)
	operations, operationIDSet, err := s.loadLinkedOperationsLocked(effectiveOperationIDs)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateReservationOperationLinksLocked(reservations, operationIDSet); err != nil {
		return nil, nil, err
	}

	updatedReservations := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		reservation.Status = StatusUnknown
		reservation.TxHash = update.TxHash
		reservation.TxBytesHash = update.TxBytesHash
		reservation.SignDocHash = update.SignDocHash
		reservation.AccountSequence = update.AccountSequence
		reservation.BroadcastAttemptCount++
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
		operation.TxHash = update.TxHash
		operation.TxBytesHash = update.TxBytesHash
		operation.SignDocHash = update.SignDocHash
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
		if err := requireLeaseToken(reservation, ref.LeaseToken, now); err != nil {
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
		strings.TrimSpace(update.TxBytesHash) != "" ||
		strings.TrimSpace(update.SignDocHash) != ""
}

func broadcastAttemptHasTxIdentity(update BroadcastAttemptUpdate) bool {
	return strings.TrimSpace(update.TxHash) != "" ||
		strings.TrimSpace(update.TxBytesHash) != "" ||
		strings.TrimSpace(update.SignDocHash) != ""
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
		if reservation.OperationID == "" || !IsActiveReservationStatus(reservation.Status) {
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
	if existing != incoming {
		return "", fmt.Errorf("%w: %s mismatch", ErrInvalidReservation, fieldName)
	}
	return existing, nil
}

func (s *MemoryStore) UpdateReservation(_ context.Context, reservation NoteReservation) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()

	if _, ok := s.reservations[reservation.ReservationID]; !ok {
		return nil, ErrReservationNotFound
	}
	if IsActiveReservationStatus(reservation.Status) {
		if s.activeReservationConflictLocked(reservation, reservation.ReservationID) {
			return nil, ErrActiveReservationExists
		}
	}
	s.storeReservationLocked(reservation)
	updated := cloneReservation(reservation)
	return &updated, nil
}

func (s *MemoryStore) CreateOperation(_ context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validateOperationCreateLocked(operation, nil); err != nil {
		return nil, err
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

func (s *MemoryStore) UpdateOperation(_ context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *MemoryStore) ensureMapsLocked() {
	if s.reservations == nil {
		s.reservations = make(map[string]NoteReservation)
	}
	if s.operations == nil {
		s.operations = make(map[string]PayrollOperation)
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
