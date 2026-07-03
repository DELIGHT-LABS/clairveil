package reservation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func (s Service) AcquireLease(ctx context.Context, reservationID string, owner string, ttl time.Duration) (*Lease, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if owner == "" {
		return nil, fmt.Errorf("lease owner is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("lease ttl must be positive")
	}

	current, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if !IsActiveReservationStatus(current.Status) {
		return nil, fmt.Errorf("%w: status %s is not active", ErrLeaseUnavailable, current.Status)
	}
	if current.LeaseToken != "" && current.LeaseUntil.After(now) {
		return nil, ErrLeaseUnavailable
	}

	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	current.LeaseOwner = owner
	current.LeaseToken = token
	current.LeaseUntil = now.Add(ttl)
	current.LastHeartbeatAt = now
	current.UpdatedAt = now
	updated, err := s.Store.UpdateReservation(ctx, *current)
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
	if ttl <= 0 {
		return nil, fmt.Errorf("lease ttl must be positive")
	}
	current, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := requireLeaseToken(*current, token, now); err != nil {
		return nil, err
	}
	current.LeaseUntil = now.Add(ttl)
	current.LastHeartbeatAt = now
	current.UpdatedAt = now
	updated, err := s.Store.UpdateReservation(ctx, *current)
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
	current, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	if err := requireLeaseToken(*current, token, s.now()); err != nil {
		return nil, err
	}
	current.LeaseOwner = ""
	current.LeaseToken = ""
	current.LeaseUntil = time.Time{}
	current.UpdatedAt = s.now()
	return s.Store.UpdateReservation(ctx, *current)
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
