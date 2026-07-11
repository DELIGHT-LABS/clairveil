package reservation

import (
	"context"
	"time"
)

var _ BatchOperationStore = (*DurableFileStore)(nil)

func (s *DurableFileStore) CreateBatchOperation(ctx context.Context, reservations []NoteReservation, graph BatchOperationGraph) (*BatchOperationGraph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	created, err := s.memory.CreateBatchOperation(ctx, reservations, graph)
	if err != nil {
		return nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *DurableFileStore) GetBatchOperation(ctx context.Context, operationID string) (*BatchOperationGraph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	return s.memory.GetBatchOperation(ctx, operationID)
}

func (s *DurableFileStore) AcquireBatchOperationLease(ctx context.Context, operationID, owner, token string, leaseUntil, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func() (*BatchOperation, error) {
		return s.memory.AcquireBatchOperationLease(ctx, operationID, owner, token, leaseUntil, now)
	})
}

func (s *DurableFileStore) HeartbeatBatchOperationLease(ctx context.Context, operationID, token string, leaseUntil, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func() (*BatchOperation, error) {
		return s.memory.HeartbeatBatchOperationLease(ctx, operationID, token, leaseUntil, now)
	})
}

func (s *DurableFileStore) ReleaseBatchOperationLease(ctx context.Context, operationID, token string, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func() (*BatchOperation, error) {
		return s.memory.ReleaseBatchOperationLease(ctx, operationID, token, now)
	})
}

func (s *DurableFileStore) CompareAndSetBatchOperationStatus(ctx context.Context, operationID, leaseToken string, from, to OperationStatus, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func() (*BatchOperation, error) {
		return s.memory.CompareAndSetBatchOperationStatus(ctx, operationID, leaseToken, from, to, now)
	})
}

func (s *DurableFileStore) PrepareBatchOperationResign(ctx context.Context, operationID, leaseToken, failedTxHash string, failureCode uint32, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func() (*BatchOperation, error) {
		return s.memory.PrepareBatchOperationResign(ctx, operationID, leaseToken, failedTxHash, failureCode, now)
	})
}

func (s *DurableFileStore) SaveBatchProofArtifacts(ctx context.Context, operationID, leaseToken string, update BatchProofArtifactUpdate, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func() (*BatchOperation, error) {
		return s.memory.SaveBatchProofArtifacts(ctx, operationID, leaseToken, update, now)
	})
}

func (s *DurableFileStore) SaveBatchSignedTx(ctx context.Context, operationID, leaseToken string, update BatchSignedTxUpdate, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func() (*BatchOperation, error) {
		return s.memory.SaveBatchSignedTx(ctx, operationID, leaseToken, update, now)
	})
}

func (s *DurableFileStore) RecordBatchBroadcast(ctx context.Context, operationID, leaseToken string, update BatchBroadcastUpdate, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func() (*BatchOperation, error) {
		return s.memory.RecordBatchBroadcast(ctx, operationID, leaseToken, update, now)
	})
}

func (s *DurableFileStore) ReconcileBatchOperation(ctx context.Context, operationID string, update BatchReconcileUpdate, now time.Time) (*BatchOperationGraph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.ReconcileBatchOperation(ctx, operationID, update, now)
	if err != nil {
		return nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *DurableFileStore) mutateBatchOperation(ctx context.Context, mutate func() (*BatchOperation, error)) (*BatchOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := mutate()
	if err != nil {
		return nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, err
	}
	return updated, nil
}
