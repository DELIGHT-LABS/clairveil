package reservation

import (
	"context"
	"time"
)

type Store interface {
	CreateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error)
	CreateReservationBatch(ctx context.Context, reservations []NoteReservation, operations []PayrollOperation) ([]NoteReservation, error)
	GetReservation(ctx context.Context, reservationID string) (*NoteReservation, error)
	ListReservations(ctx context.Context, filter ReservationFilter) ([]NoteReservation, error)
	CompareAndSetReservationStatus(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error)
	CompareAndSetReservationStatusWithLease(ctx context.Context, reservationID string, leaseToken string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error)
	AcquireReservationLease(ctx context.Context, reservationID string, owner string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error)
	AcquireReservationLeaseForStatus(ctx context.Context, reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error)
	HeartbeatReservationLease(ctx context.Context, reservationID string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error)
	HeartbeatReservationLeaseForStatus(ctx context.Context, reservationID string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error)
	ClearReservationLease(ctx context.Context, reservationID string, leaseToken string, now time.Time) (*NoteReservation, error)
	MarkReservationsProofReady(ctx context.Context, reservations []SubmittedReservationRef, operationUpdate ProofReadyOperationUpdate, now time.Time) ([]NoteReservation, *PayrollOperation, error)
	MarkReservationSubmitted(ctx context.Context, reservationID string, leaseToken string, update SubmittedReservationUpdate, now time.Time) (*NoteReservation, error)
	MarkReservationsSubmitted(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update SubmittedReservationUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error)
	UpdateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error)

	CreateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error)
	GetOperation(ctx context.Context, operationID string) (*PayrollOperation, error)
	UpdateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error)
}
