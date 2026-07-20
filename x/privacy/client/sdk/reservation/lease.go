package reservation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func (s Service) AcquireLease(ctx context.Context, reservationID string, owner string, ttl time.Duration) (*Lease, error) {
	return s.acquireLease(ctx, reservationID, owner, "", ttl)
}

func (s Service) AcquireLeaseForStatus(ctx context.Context, reservationID string, owner string, status ReservationStatus, ttl time.Duration) (*Lease, error) {
	return s.acquireLease(ctx, reservationID, owner, status, ttl)
}

func (s Service) acquireLease(ctx context.Context, reservationID string, owner string, status ReservationStatus, ttl time.Duration) (*Lease, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("lease owner is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("lease ttl must be positive")
	}
	now := s.now()
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(ttl)
	updated, err := s.Store.AcquireSingleReservationLease(
		ctx,
		reservationID,
		owner,
		token,
		status,
		leaseUntil,
		now,
	)
	if err != nil {
		return nil, err
	}

	return &Lease{
		Owner: updated.LeaseOwner,
		Token: updated.LeaseToken,
		Until: updated.LeaseUntil,
	}, nil
}

// BeginProvingOperation atomically leases and claims every Reserved input of
// one operation. A caller never observes a partially Proving operation.
func (s Service) BeginProvingOperation(ctx context.Context, operationID string, reservationIDs []string, owner string, ttl time.Duration) ([]SubmittedReservationRef, *PayrollOperation, error) {
	if s.Store == nil {
		return nil, nil, fmt.Errorf("reservation store is required")
	}
	operationID = strings.TrimSpace(operationID)
	owner = strings.TrimSpace(owner)
	if operationID == "" {
		return nil, nil, fmt.Errorf("operation_id is required")
	}
	if owner == "" {
		return nil, nil, fmt.Errorf("lease owner is required")
	}
	if ttl <= 0 {
		return nil, nil, fmt.Errorf("lease ttl must be positive")
	}
	if len(reservationIDs) == 0 {
		return nil, nil, fmt.Errorf("reservation ids are required")
	}

	refs := make([]SubmittedReservationRef, 0, len(reservationIDs))
	for _, reservationID := range reservationIDs {
		token, err := randomToken()
		if err != nil {
			return nil, nil, err
		}
		refs = append(refs, SubmittedReservationRef{
			ReservationID: strings.TrimSpace(reservationID),
			LeaseOwner:    owner,
			LeaseToken:    token,
		})
	}
	now := s.now()
	reservations, operation, err := s.Store.BeginProvingOperation(ctx, operationID, refs, now.Add(ttl), now)
	if err != nil {
		return nil, nil, err
	}
	claimed := make([]SubmittedReservationRef, 0, len(reservations))
	for _, reservation := range reservations {
		claimed = append(claimed, SubmittedReservationRef{
			ReservationID: reservation.ReservationID,
			LeaseOwner:    reservation.LeaseOwner,
			LeaseToken:    reservation.LeaseToken,
		})
	}
	return claimed, operation, nil
}

// RollbackProvingOperation atomically returns every claimed input and its
// operation to the pre-proof state while clearing the worker leases.
func (s Service) RollbackProvingOperation(ctx context.Context, operationID string, refs []SubmittedReservationRef) ([]NoteReservation, *PayrollOperation, error) {
	if s.Store == nil {
		return nil, nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.RollbackProvingOperation(ctx, strings.TrimSpace(operationID), refs, s.now())
}

func (s Service) HeartbeatLease(ctx context.Context, reservationID string, owner string, token string, ttl time.Duration) (*Lease, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("lease ttl must be positive")
	}
	now := s.now()
	updated, err := s.Store.HeartbeatReservationLease(ctx, reservationID, owner, token, now.Add(ttl), now)
	if err != nil {
		return nil, err
	}
	return &Lease{
		Owner: updated.LeaseOwner,
		Token: updated.LeaseToken,
		Until: updated.LeaseUntil,
	}, nil
}

func (s Service) ClearLease(ctx context.Context, reservationID string, owner string, token string) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.ClearReservationLease(ctx, reservationID, owner, token, s.now())
}

func requireLeaseToken(reservation NoteReservation, owner string, token string, now time.Time) error {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(owner) != reservation.LeaseOwner || token == "" || token != reservation.LeaseToken {
		return ErrLeaseMismatch
	}
	if reservation.LeaseUntil.IsZero() || !reservation.LeaseUntil.After(now) {
		return ErrLeaseUnavailable
	}
	return nil
}

func randomToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
