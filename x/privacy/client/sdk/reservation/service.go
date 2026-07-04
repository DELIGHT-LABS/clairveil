package reservation

import (
	"context"
	"fmt"
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
		if _, err := s.Store.GetOperation(ctx, reservation.OperationID); err != nil {
			return fmt.Errorf("%w: reservation %s references missing operation %s", ErrOperationNotFound, reservation.ReservationID, reservation.OperationID)
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
	return s.Store.CompareAndSetReservationStatus(ctx, reservationID, from, to, s.now())
}

func (s Service) TransitionWithLease(ctx context.Context, reservationID string, leaseToken string, from ReservationStatus, to ReservationStatus) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if !CanTransitionReservation(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return s.Store.CompareAndSetReservationStatusWithLease(ctx, reservationID, leaseToken, from, to, s.now())
}

func (s Service) MarkSubmitted(ctx context.Context, reservationID string, leaseToken string, txHash string, txBytesHash string, signDocHash string, accountSequence uint64) (*NoteReservation, error) {
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

func (s Service) HeartbeatLeaseForStatus(ctx context.Context, reservationID string, token string, status ReservationStatus, ttl time.Duration) (*Lease, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("lease ttl must be positive")
	}
	now := s.now()
	updated, err := s.Store.HeartbeatReservationLeaseForStatus(ctx, reservationID, token, status, now.Add(ttl), now)
	if err != nil {
		return nil, err
	}
	return &Lease{
		Owner: updated.LeaseOwner,
		Token: updated.LeaseToken,
		Until: updated.LeaseUntil,
	}, nil
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

func (s Service) MarkProofReadyBatch(ctx context.Context, refs []SubmittedReservationRef, update ProofReadyOperationUpdate) ([]NoteReservation, *PayrollOperation, error) {
	if s.Store == nil {
		return nil, nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.MarkReservationsProofReady(ctx, refs, update, s.now())
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
