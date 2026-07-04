package reservation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	var updated *NoteReservation
	if status == "" {
		updated, err = s.Store.AcquireReservationLease(ctx, reservationID, owner, token, leaseUntil, now)
	} else {
		updated, err = s.Store.AcquireReservationLeaseForStatus(ctx, reservationID, owner, token, status, leaseUntil, now)
	}
	if err != nil {
		return nil, err
	}

	return &Lease{
		Owner: updated.LeaseOwner,
		Token: updated.LeaseToken,
		Until: updated.LeaseUntil,
	}, nil
}

func (s Service) HeartbeatLease(ctx context.Context, reservationID string, token string, ttl time.Duration) (*Lease, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("lease ttl must be positive")
	}
	now := s.now()
	updated, err := s.Store.HeartbeatReservationLease(ctx, reservationID, token, now.Add(ttl), now)
	if err != nil {
		return nil, err
	}
	return &Lease{
		Owner: updated.LeaseOwner,
		Token: updated.LeaseToken,
		Until: updated.LeaseUntil,
	}, nil
}

func (s Service) ClearLease(ctx context.Context, reservationID string, token string) (*NoteReservation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	return s.Store.ClearReservationLease(ctx, reservationID, token, s.now())
}

func requireLeaseToken(reservation NoteReservation, token string, now time.Time) error {
	if token == "" || token != reservation.LeaseToken {
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
