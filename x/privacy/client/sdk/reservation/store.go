package reservation

import (
	"context"
	"time"
)

type Store interface {
	CreateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error)
	// CreateReservationBatch must atomically reject extending an existing
	// operation after the operation or any linked reservation has acquired
	// lifecycle evidence. Operation membership is mutable only while the
	// operation is pristine Planned and every existing member is pristine
	// Reserved; this prevents workers from observing a partial input set.
	CreateReservationBatch(ctx context.Context, reservations []NoteReservation, operations []PayrollOperation) ([]NoteReservation, error)
	GetReservation(ctx context.Context, reservationID string) (*NoteReservation, error)
	ListReservations(ctx context.Context, filter ReservationFilter) ([]NoteReservation, error)
	CompareAndSetReservationStatus(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error)
	CompareAndSetReservationStatusWithLease(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error)
	ApplyReconciliationTransition(ctx context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error)
	ApplyLeaseExpiryRecovery(ctx context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error)
	ApplyProofDiscardTransition(ctx context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error)
	// AcquireSingleReservationLease must atomically acquire the lease and reject
	// the request when the reservation belongs to an operation with more than
	// one linked reservation. This prevents a concurrent operation extension
	// from creating a partially leased multi-input operation.
	AcquireSingleReservationLease(ctx context.Context, reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error)
	AcquireReservationLease(ctx context.Context, reservationID string, owner string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error)
	AcquireReservationLeaseForStatus(ctx context.Context, reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error)
	BeginProvingOperation(ctx context.Context, operationID string, reservations []SubmittedReservationRef, leaseUntil time.Time, now time.Time) ([]NoteReservation, *PayrollOperation, error)
	RollbackProvingOperation(ctx context.Context, operationID string, reservations []SubmittedReservationRef, now time.Time) ([]NoteReservation, *PayrollOperation, error)
	HeartbeatReservationLease(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error)
	HeartbeatReservationLeaseForStatus(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error)
	RecordRelayHandoff(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, payloadHash string, now time.Time) (*NoteReservation, error)
	RecordRelayHandoffBatch(ctx context.Context, operationID string, refs []SubmittedReservationRef, payloadHash string, now time.Time) ([]NoteReservation, error)
	ClearReservationLease(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, now time.Time) (*NoteReservation, error)
	MarkReservationsProofReady(ctx context.Context, reservations []SubmittedReservationRef, operationUpdate ProofReadyOperationUpdate, now time.Time) ([]NoteReservation, *PayrollOperation, error)
	MarkReservationsProofDiscarding(ctx context.Context, operationID string, reservations []SubmittedReservationRef, now time.Time) ([]NoteReservation, error)
	MarkReservationsBroadcastAttempting(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptStart, now time.Time) ([]NoteReservation, []PayrollOperation, error)
	MarkReservationSubmitted(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, update SubmittedReservationUpdate, now time.Time) (*NoteReservation, error)
	MarkReservationsSubmitted(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update SubmittedReservationUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error)
	MarkReservationsBroadcastUnknown(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error)
	MarkReservationsBroadcastAmbiguous(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update BroadcastAmbiguityUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error)
	MarkReservationsProofArtifactCleanupFailed(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, reason string, now time.Time) ([]NoteReservation, []PayrollOperation, error)
	CreateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error)
	GetOperation(ctx context.Context, operationID string) (*PayrollOperation, error)
}
