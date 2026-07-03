package reservation

import (
	"context"
	"time"
)

type Store interface {
	CreateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error)
	GetReservation(ctx context.Context, reservationID string) (*NoteReservation, error)
	ListReservations(ctx context.Context, filter ReservationFilter) ([]NoteReservation, error)
	CompareAndSetReservationStatus(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error)
	UpdateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error)

	CreateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error)
	GetOperation(ctx context.Context, operationID string) (*PayrollOperation, error)
	UpdateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error)
}
