package reservation

import (
	"context"
	"time"
)

// BatchOperationStore extends the ordinary reservation store with the
// many-input/one-operation/many-output persistence required by one-proof
// BatchJoinSplit16x32 payroll execution.
type BatchOperationStore interface {
	CreateBatchOperation(ctx context.Context, reservations []NoteReservation, graph BatchOperationGraph) (*BatchOperationGraph, error)
	GetBatchOperation(ctx context.Context, operationID string) (*BatchOperationGraph, error)
	AcquireBatchOperationLease(ctx context.Context, operationID, owner, token string, leaseUntil, now time.Time) (*BatchOperation, error)
	HeartbeatBatchOperationLease(ctx context.Context, operationID, token string, leaseUntil, now time.Time) (*BatchOperation, error)
	ReleaseBatchOperationLease(ctx context.Context, operationID, token string, now time.Time) (*BatchOperation, error)
	// CompareAndSetBatchOperationStatus only claims or rolls back the proving
	// state. Proof, broadcast, and reconcile transitions use their dedicated
	// atomic methods so operation and input-reservation relations stay aligned.
	CompareAndSetBatchOperationStatus(ctx context.Context, operationID, leaseToken string, from, to OperationStatus, now time.Time) (*BatchOperation, error)
	SaveBatchProofArtifacts(ctx context.Context, operationID, leaseToken string, update BatchProofArtifactUpdate, now time.Time) (*BatchOperation, error)
	SaveBatchSignedTx(ctx context.Context, operationID, leaseToken string, update BatchSignedTxUpdate, now time.Time) (*BatchOperation, error)
	RecordBatchBroadcast(ctx context.Context, operationID, leaseToken string, update BatchBroadcastUpdate, now time.Time) (*BatchOperation, error)
	ReconcileBatchOperation(ctx context.Context, operationID string, update BatchReconcileUpdate, now time.Time) (*BatchOperationGraph, error)
}
