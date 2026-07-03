package reservation

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu           sync.Mutex
	reservations map[string]NoteReservation
	operations   map[string]PayrollOperation
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		reservations: make(map[string]NoteReservation),
		operations:   make(map[string]PayrollOperation),
	}
}

func (s *MemoryStore) CreateReservation(_ context.Context, reservation NoteReservation) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if reservation.ReservationID == "" {
		return nil, fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
	}
	if _, exists := s.reservations[reservation.ReservationID]; exists {
		return nil, fmt.Errorf("%w: reservation_id already exists", ErrActiveReservationExists)
	}
	if IsActiveReservationStatus(reservation.Status) {
		for _, existing := range s.reservations {
			if existing.ReservationID == reservation.ReservationID {
				continue
			}
			if IsActiveReservationStatus(existing.Status) && existing.OwnerKeyID == reservation.OwnerKeyID && existing.NullifierLookupKey == reservation.NullifierLookupKey {
				return nil, ErrActiveReservationExists
			}
		}
	}
	s.reservations[reservation.ReservationID] = cloneReservation(reservation)
	created := cloneReservation(reservation)
	return &created, nil
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
	reservation.Status = to
	reservation.UpdatedAt = now
	s.reservations[reservationID] = reservation
	cloned := cloneReservation(reservation)
	return &cloned, nil
}

func (s *MemoryStore) UpdateReservation(_ context.Context, reservation NoteReservation) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.reservations[reservation.ReservationID]; !ok {
		return nil, ErrReservationNotFound
	}
	if IsActiveReservationStatus(reservation.Status) {
		for _, existing := range s.reservations {
			if existing.ReservationID == reservation.ReservationID {
				continue
			}
			if IsActiveReservationStatus(existing.Status) && existing.OwnerKeyID == reservation.OwnerKeyID && existing.NullifierLookupKey == reservation.NullifierLookupKey {
				return nil, ErrActiveReservationExists
			}
		}
	}
	s.reservations[reservation.ReservationID] = cloneReservation(reservation)
	updated := cloneReservation(reservation)
	return &updated, nil
}

func (s *MemoryStore) CreateOperation(_ context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if operation.OperationID == "" {
		return nil, fmt.Errorf("operation_id is required")
	}
	if _, exists := s.operations[operation.OperationID]; exists {
		return nil, fmt.Errorf("operation already exists")
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

func cloneReservation(reservation NoteReservation) NoteReservation {
	reservation.EncryptedNullifier = append([]byte(nil), reservation.EncryptedNullifier...)
	return reservation
}

func cloneOperation(operation PayrollOperation) PayrollOperation {
	operation.EncryptedExpectedRecipient = append([]byte(nil), operation.EncryptedExpectedRecipient...)
	operation.EncryptedExpectedAmount = append([]byte(nil), operation.EncryptedExpectedAmount...)
	return operation
}
