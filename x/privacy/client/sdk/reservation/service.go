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

	return s.Store.CreateReservationBatch(ctx, reservations, operations)
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
		if operation.ReservationID == "" {
			operation.ReservationID = reservation.ReservationID
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
	current, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	if current.Status != StatusProofReady {
		return nil, fmt.Errorf("%w: expected ProofReady got %s", ErrCompareAndSetFailed, current.Status)
	}
	if err := requireLeaseToken(*current, leaseToken, s.now()); err != nil {
		return nil, err
	}
	current.Status = StatusSubmitted
	current.TxHash = txHash
	current.TxBytesHash = txBytesHash
	current.SignDocHash = signDocHash
	current.AccountSequence = accountSequence
	current.BroadcastAttemptCount++
	current.LastBroadcastAt = s.now()
	current.UpdatedAt = current.LastBroadcastAt
	return s.Store.UpdateReservation(ctx, *current)
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
