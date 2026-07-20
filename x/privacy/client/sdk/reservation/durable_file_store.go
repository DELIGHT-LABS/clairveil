package reservation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const durableFileStoreVersion = 1

type DurableFileStore struct {
	mu     sync.Mutex
	path   string
	memory *MemoryStore
}

type DurableFileStoreSnapshot struct {
	Version            int                         `json:"version"`
	BatchSchemaVersion string                      `json:"batch_schema_version,omitempty"`
	UpdatedAt          time.Time                   `json:"updated_at"`
	Reservations       []NoteReservation           `json:"reservations"`
	Operations         []PayrollOperation          `json:"operations"`
	BatchOperations    []BatchOperation            `json:"batch_operations,omitempty"`
	BatchInputs        []OperationInputReservation `json:"batch_inputs,omitempty"`
	BatchItems         []PayrollItemOutput         `json:"batch_items,omitempty"`
	BatchEvidence      []ExpectedOutputEvidence    `json:"batch_evidence,omitempty"`
}

func OpenDurableFileStore(path string) (*DurableFileStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: durable reservation store path is required", ErrInvalidReservation)
	}
	memory, err := loadDurableFileStoreSnapshot(path)
	if err != nil {
		return nil, err
	}
	store := &DurableFileStore{
		path:   path,
		memory: memory,
	}
	return store, nil
}

func (s *DurableFileStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *DurableFileStore) Snapshot(ctx context.Context) (*DurableFileStoreSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	snapshot := s.snapshotLocked()
	return &snapshot, nil
}

func ensureDurableFileDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}

func (s *DurableFileStore) refreshLocked(ctx context.Context) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := s.acquireFileLockLocked()
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, unlock())
	}()
	snapshot, err := readDurableFileStoreSnapshot(s.path)
	if err != nil {
		return err
	}
	memory, err := memoryStoreFromSnapshot(snapshot)
	if err != nil {
		return err
	}
	s.memory = memory
	return err
}

func (s *DurableFileStore) CreateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	created, err := s.memory.CreateReservation(ctx, reservation)
	if err != nil {
		return nil, err
	}
	return created, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) CreateReservationBatch(ctx context.Context, reservations []NoteReservation, operations []PayrollOperation) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	created, err := s.memory.CreateReservationBatch(ctx, reservations, operations)
	if err != nil {
		return nil, err
	}
	return created, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) GetReservation(ctx context.Context, reservationID string) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	return s.memory.GetReservation(ctx, reservationID)
}

func (s *DurableFileStore) ListReservations(ctx context.Context, filter ReservationFilter) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	return s.memory.ListReservations(ctx, filter)
}

func (s *DurableFileStore) CompareAndSetReservationStatus(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.CompareAndSetReservationStatus(ctx, reservationID, from, to, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) CompareAndSetReservationStatusWithOperation(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus, operation *PayrollOperation, now time.Time) (*NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservation, updatedOperation, err := s.memory.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:     reservationID,
		From:              from,
		To:                to,
		Operation:         operation,
		Now:               now,
		serviceAuthorized: true,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservation, updatedOperation, nil
}

func (s *DurableFileStore) CompareAndSetReservationStatusWithLease(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.CompareAndSetReservationStatusWithLease(ctx, reservationID, leaseOwner, leaseToken, from, to, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) ApplyReconciliationTransition(ctx context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservation, updatedOperation, err := s.memory.ApplyReconciliationTransition(ctx, transition)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservation, updatedOperation, nil
}

func (s *DurableFileStore) ApplyLeaseExpiryRecovery(ctx context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservation, updatedOperation, err := s.memory.ApplyLeaseExpiryRecovery(ctx, transition)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservation, updatedOperation, nil
}

func (s *DurableFileStore) ApplyProofDiscardTransition(ctx context.Context, transition ReconciliationTransition) (*NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservation, updatedOperation, err := s.memory.ApplyProofDiscardTransition(ctx, transition)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservation, updatedOperation, nil
}

func (s *DurableFileStore) AcquireReservationLease(ctx context.Context, reservationID string, owner string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.AcquireReservationLease(ctx, reservationID, owner, leaseToken, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) AcquireReservationLeaseForStatus(ctx context.Context, reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.AcquireReservationLeaseForStatus(ctx, reservationID, owner, leaseToken, requiredStatus, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) AcquireSingleReservationLease(ctx context.Context, reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.AcquireSingleReservationLease(ctx, reservationID, owner, leaseToken, requiredStatus, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) BeginProvingOperation(ctx context.Context, operationID string, reservations []SubmittedReservationRef, leaseUntil time.Time, now time.Time) ([]NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservations, updatedOperation, err := s.memory.BeginProvingOperation(ctx, operationID, reservations, leaseUntil, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperation, nil
}

func (s *DurableFileStore) ReclaimExpiredOperation(ctx context.Context, operationID string, reservations []SubmittedReservationRef, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) ([]NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservations, updatedOperation, err := s.memory.ReclaimExpiredOperation(ctx, operationID, reservations, requiredStatus, leaseUntil, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperation, nil
}

func (s *DurableFileStore) RollbackProvingOperation(ctx context.Context, operationID string, reservations []SubmittedReservationRef, now time.Time) ([]NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservations, updatedOperation, err := s.memory.RollbackProvingOperation(ctx, operationID, reservations, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperation, nil
}

func (s *DurableFileStore) HeartbeatReservationLease(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.HeartbeatReservationLease(ctx, reservationID, leaseOwner, leaseToken, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) HeartbeatReservationLeaseForStatus(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.HeartbeatReservationLeaseForStatus(ctx, reservationID, leaseOwner, leaseToken, requiredStatus, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) ClearReservationLease(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.ClearReservationLease(ctx, reservationID, leaseOwner, leaseToken, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) MarkReservationsProofReady(ctx context.Context, reservations []SubmittedReservationRef, operationUpdate ProofReadyOperationUpdate, now time.Time) ([]NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservations, updatedOperation, err := s.memory.MarkReservationsProofReady(ctx, reservations, operationUpdate, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperation, nil
}

func (s *DurableFileStore) RecordRelayHandoff(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, payloadHash string, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.RecordRelayHandoff(ctx, reservationID, leaseOwner, leaseToken, payloadHash, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) RecordRelayHandoffBatch(ctx context.Context, operationID string, refs []SubmittedReservationRef, payloadHash string, now time.Time) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.RecordRelayHandoffBatch(ctx, operationID, refs, payloadHash, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) MarkReservationsProofDiscarding(ctx context.Context, operationID string, reservations []SubmittedReservationRef, now time.Time) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.MarkReservationsProofDiscarding(ctx, operationID, reservations, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) MarkReservationSubmitted(ctx context.Context, reservationID string, leaseOwner string, leaseToken string, update SubmittedReservationUpdate, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.MarkReservationSubmitted(ctx, reservationID, leaseOwner, leaseToken, update, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) MarkReservationsBroadcastAttempting(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptStart, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservations, updatedOperations, err := s.memory.MarkReservationsBroadcastAttempting(ctx, reservations, operationIDs, update, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperations, nil
}

func (s *DurableFileStore) MarkReservationsSubmitted(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update SubmittedReservationUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservations, updatedOperations, err := s.memory.MarkReservationsSubmitted(ctx, reservations, operationIDs, update, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperations, nil
}

func (s *DurableFileStore) MarkReservationsBroadcastUnknown(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservations, updatedOperations, err := s.memory.MarkReservationsBroadcastUnknown(ctx, reservations, operationIDs, update, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperations, nil
}

func (s *DurableFileStore) MarkReservationsBroadcastAmbiguous(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update BroadcastAmbiguityUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservations, updatedOperations, err := s.memory.MarkReservationsBroadcastAmbiguous(ctx, reservations, operationIDs, update, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperations, nil
}

func (s *DurableFileStore) MarkReservationsProofArtifactCleanupFailed(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, reason string, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, nil, err
	}
	before := s.snapshotLocked()
	updatedReservations, updatedOperations, err := s.memory.MarkReservationsProofArtifactCleanupFailed(ctx, reservations, operationIDs, reason, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistMutationLocked(ctx, before); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperations, nil
}

func (s *DurableFileStore) UpdateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.UpdateReservation(ctx, reservation)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) CreateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	created, err := s.memory.CreateOperation(ctx, operation)
	if err != nil {
		return nil, err
	}
	return created, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) GetOperation(ctx context.Context, operationID string) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	return s.memory.GetOperation(ctx, operationID)
}

func (s *DurableFileStore) UpdateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	before := s.snapshotLocked()
	updated, err := s.memory.UpdateOperation(ctx, operation)
	if err != nil {
		return nil, err
	}
	return updated, s.persistMutationLocked(ctx, before)
}

func (s *DurableFileStore) persist(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked(ctx)
}

func (s *DurableFileStore) persistLocked(ctx context.Context) (err error) {
	unlock, err := s.acquireFileLockLocked()
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, unlock())
	}()
	err = s.persistLockedWithFileLock(ctx)
	return err
}

func (s *DurableFileStore) persistLockedWithFileLock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("%w: durable reservation store path is required", ErrInvalidReservation)
	}
	if err := ensureDurableFileDir(s.path); err != nil {
		return err
	}
	snapshot := s.snapshotLocked()
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func (s *DurableFileStore) persistMutationLocked(ctx context.Context, before DurableFileStoreSnapshot) error {
	unlock, err := s.acquireFileLockLocked()
	if err == nil {
		if matchErr := s.ensureOnDiskSnapshotMatchesLocked(before); matchErr != nil {
			err = matchErr
		} else {
			err = s.persistLockedWithFileLock(ctx)
		}
		err = errors.Join(err, unlock())
	}
	if err != nil {
		restored, restoreErr := memoryStoreFromSnapshot(before)
		if restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		s.memory = restored
		return err
	}
	return nil
}

func (s *DurableFileStore) acquireFileLockLocked() (func() error, error) {
	if strings.TrimSpace(s.path) == "" {
		return nil, fmt.Errorf("%w: durable reservation store path is required", ErrInvalidReservation)
	}
	if err := ensureDurableFileDir(s.path); err != nil {
		return nil, err
	}
	lockPath := s.path + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() error {
		unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		closeErr := file.Close()
		return errors.Join(unlockErr, closeErr)
	}, nil
}

func (s *DurableFileStore) ensureOnDiskSnapshotMatchesLocked(expected DurableFileStoreSnapshot) error {
	onDisk, err := readDurableFileStoreSnapshot(s.path)
	if err != nil {
		return err
	}
	if !durableSnapshotsEquivalent(expected, onDisk) {
		return fmt.Errorf("%w: durable reservation state changed on disk", ErrCompareAndSetFailed)
	}
	return nil
}

func (s *DurableFileStore) snapshotLocked() DurableFileStoreSnapshot {
	s.memory.mu.Lock()
	defer s.memory.mu.Unlock()
	s.memory.ensureMapsLocked()

	reservations := make([]NoteReservation, 0, len(s.memory.reservations))
	for _, reservation := range s.memory.reservations {
		reservations = append(reservations, cloneReservation(reservation))
	}
	sort.Slice(reservations, func(i, j int) bool {
		return reservations[i].ReservationID < reservations[j].ReservationID
	})

	operations := make([]PayrollOperation, 0, len(s.memory.operations))
	for _, operation := range s.memory.operations {
		operations = append(operations, cloneOperation(operation))
	}
	sort.Slice(operations, func(i, j int) bool {
		return operations[i].OperationID < operations[j].OperationID
	})
	batchOperations := make([]BatchOperation, 0, len(s.memory.batchOperations))
	batchInputs := make([]OperationInputReservation, 0)
	batchItems := make([]PayrollItemOutput, 0)
	batchEvidence := make([]ExpectedOutputEvidence, 0)
	for operationID, operation := range s.memory.batchOperations {
		batchOperations = append(batchOperations, cloneBatchOperation(operation))
		batchInputs = append(batchInputs, cloneBatchInputs(s.memory.batchInputs[operationID])...)
		batchItems = append(batchItems, cloneBatchItems(s.memory.batchItems[operationID])...)
		batchEvidence = append(batchEvidence, cloneBatchEvidence(s.memory.batchEvidence[operationID])...)
	}
	sortBatchSnapshotRelations(batchOperations, batchInputs, batchItems, batchEvidence)

	return DurableFileStoreSnapshot{
		Version:            durableFileStoreVersion,
		BatchSchemaVersion: BatchOperationSchemaVersionV1,
		UpdatedAt:          time.Now().UTC(),
		Reservations:       reservations,
		Operations:         operations,
		BatchOperations:    batchOperations,
		BatchInputs:        batchInputs,
		BatchItems:         batchItems,
		BatchEvidence:      batchEvidence,
	}
}

func loadDurableFileStoreSnapshot(path string) (*MemoryStore, error) {
	snapshot, err := readDurableFileStoreSnapshot(path)
	if err != nil {
		return nil, err
	}
	return memoryStoreFromSnapshot(snapshot)
}

func readDurableFileStoreSnapshot(path string) (DurableFileStoreSnapshot, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DurableFileStoreSnapshot{Version: durableFileStoreVersion}, nil
		}
		return DurableFileStoreSnapshot{}, err
	}
	var snapshot DurableFileStoreSnapshot
	decoder := json.NewDecoder(bytes.NewReader(bz))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return DurableFileStoreSnapshot{}, err
	}
	if snapshot.Version != durableFileStoreVersion {
		return DurableFileStoreSnapshot{}, fmt.Errorf("%w: unsupported durable reservation store version %d", ErrInvalidReservation, snapshot.Version)
	}
	if snapshotHasBatchData(snapshot) && snapshot.BatchSchemaVersion != BatchOperationSchemaVersionV1 {
		return DurableFileStoreSnapshot{}, fmt.Errorf("%w: unsupported batch operation schema version %q", ErrInvalidReservation, snapshot.BatchSchemaVersion)
	}
	return snapshot, nil
}

func durableSnapshotsEquivalent(left DurableFileStoreSnapshot, right DurableFileStoreSnapshot) bool {
	left = canonicalDurableSnapshot(left)
	right = canonicalDurableSnapshot(right)
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func canonicalDurableSnapshot(snapshot DurableFileStoreSnapshot) DurableFileStoreSnapshot {
	snapshot.Version = durableFileStoreVersion
	snapshot.BatchSchemaVersion = BatchOperationSchemaVersionV1
	snapshot.UpdatedAt = time.Time{}
	snapshot.Reservations = append([]NoteReservation(nil), snapshot.Reservations...)
	sort.Slice(snapshot.Reservations, func(i, j int) bool {
		return snapshot.Reservations[i].ReservationID < snapshot.Reservations[j].ReservationID
	})
	snapshot.Operations = append([]PayrollOperation(nil), snapshot.Operations...)
	sort.Slice(snapshot.Operations, func(i, j int) bool {
		return snapshot.Operations[i].OperationID < snapshot.Operations[j].OperationID
	})
	if len(snapshot.Reservations) == 0 {
		snapshot.Reservations = nil
	}
	if len(snapshot.Operations) == 0 {
		snapshot.Operations = nil
	}
	sortBatchSnapshotRelations(snapshot.BatchOperations, snapshot.BatchInputs, snapshot.BatchItems, snapshot.BatchEvidence)
	if len(snapshot.BatchOperations) == 0 {
		snapshot.BatchOperations = nil
		snapshot.BatchInputs = nil
		snapshot.BatchItems = nil
		snapshot.BatchEvidence = nil
	}
	return snapshot
}

func memoryStoreFromSnapshot(snapshot DurableFileStoreSnapshot) (*MemoryStore, error) {
	store := NewMemoryStore()
	store.mu.Lock()
	defer store.mu.Unlock()

	for _, operation := range snapshot.Operations {
		if strings.TrimSpace(operation.OperationID) == "" {
			return nil, fmt.Errorf("%w: operation_id is required", ErrInvalidReservation)
		}
		if _, exists := store.operations[operation.OperationID]; exists {
			return nil, fmt.Errorf("%w: duplicate operation_id %s", ErrInvalidReservation, operation.OperationID)
		}
		store.operations[operation.OperationID] = cloneOperation(operation)
	}
	for _, reservation := range snapshot.Reservations {
		if strings.TrimSpace(reservation.ReservationID) == "" {
			return nil, fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
		}
		if _, exists := store.reservations[reservation.ReservationID]; exists {
			return nil, fmt.Errorf("%w: duplicate reservation_id %s", ErrInvalidReservation, reservation.ReservationID)
		}
		if IsActiveReservationStatus(reservation.Status) {
			activeKey := reservation.ActiveKey()
			if existingID, exists := store.activeReservationByKey[activeKey]; exists {
				return nil, fmt.Errorf("%w: active reservation %s conflicts with %s", ErrActiveReservationExists, reservation.ReservationID, existingID)
			}
		}
		store.storeReservationLocked(reservation)
	}
	if snapshotHasBatchData(snapshot) {
		if snapshot.BatchSchemaVersion != BatchOperationSchemaVersionV1 {
			return nil, fmt.Errorf("%w: unsupported batch operation schema version %q", ErrInvalidReservation, snapshot.BatchSchemaVersion)
		}
		for _, operation := range snapshot.BatchOperations {
			if operation.SchemaVersion != BatchOperationSchemaVersionV1 || strings.TrimSpace(operation.OperationID) == "" {
				return nil, fmt.Errorf("%w: invalid persisted batch operation", ErrInvalidReservation)
			}
			if _, exists := store.batchOperations[operation.OperationID]; exists {
				return nil, fmt.Errorf("%w: duplicate batch operation_id %s", ErrInvalidReservation, operation.OperationID)
			}
			store.batchOperations[operation.OperationID] = cloneBatchOperation(operation)
		}
		for _, input := range snapshot.BatchInputs {
			store.batchInputs[input.OperationID] = append(store.batchInputs[input.OperationID], input)
		}
		for _, item := range snapshot.BatchItems {
			store.batchItems[item.OperationID] = append(store.batchItems[item.OperationID], item)
		}
		for _, evidence := range snapshot.BatchEvidence {
			store.batchEvidence[evidence.OperationID] = append(store.batchEvidence[evidence.OperationID], evidence)
		}
		if err := store.validatePersistedBatchGraphsLocked(); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func snapshotHasBatchData(snapshot DurableFileStoreSnapshot) bool {
	return len(snapshot.BatchOperations) > 0 || len(snapshot.BatchInputs) > 0 || len(snapshot.BatchItems) > 0 || len(snapshot.BatchEvidence) > 0
}

func sortBatchSnapshotRelations(operations []BatchOperation, inputs []OperationInputReservation, items []PayrollItemOutput, evidence []ExpectedOutputEvidence) {
	sort.Slice(operations, func(i, j int) bool { return operations[i].OperationID < operations[j].OperationID })
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].OperationID != inputs[j].OperationID {
			return inputs[i].OperationID < inputs[j].OperationID
		}
		return inputs[i].InputIndex < inputs[j].InputIndex
	})
	sort.Slice(items, func(i, j int) bool {
		if items[i].OperationID != items[j].OperationID {
			return items[i].OperationID < items[j].OperationID
		}
		return items[i].OutputIndex < items[j].OutputIndex
	})
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].OperationID != evidence[j].OperationID {
			return evidence[i].OperationID < evidence[j].OperationID
		}
		return evidence[i].OutputIndex < evidence[j].OutputIndex
	})
}
