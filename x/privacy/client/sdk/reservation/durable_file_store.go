package reservation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const durableFileStoreVersion = 1

type DurableFileStore struct {
	mu     sync.Mutex
	path   string
	memory *MemoryStore
}

type DurableFileStoreSnapshot struct {
	Version      int                `json:"version"`
	UpdatedAt    time.Time          `json:"updated_at"`
	Reservations []NoteReservation  `json:"reservations"`
	Operations   []PayrollOperation `json:"operations"`
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
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := store.persist(context.Background()); err != nil {
			return nil, err
		}
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
	snapshot := s.snapshotLocked()
	return &snapshot, nil
}

func (s *DurableFileStore) CreateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	created, err := s.memory.CreateReservation(ctx, reservation)
	if err != nil {
		return nil, err
	}
	return created, s.persistLocked(ctx)
}

func (s *DurableFileStore) CreateReservationBatch(ctx context.Context, reservations []NoteReservation, operations []PayrollOperation) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	created, err := s.memory.CreateReservationBatch(ctx, reservations, operations)
	if err != nil {
		return nil, err
	}
	return created, s.persistLocked(ctx)
}

func (s *DurableFileStore) GetReservation(ctx context.Context, reservationID string) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memory.GetReservation(ctx, reservationID)
}

func (s *DurableFileStore) ListReservations(ctx context.Context, filter ReservationFilter) ([]NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memory.ListReservations(ctx, filter)
}

func (s *DurableFileStore) CompareAndSetReservationStatus(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.CompareAndSetReservationStatus(ctx, reservationID, from, to, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) CompareAndSetReservationStatusWithOperation(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus, operation *PayrollOperation, now time.Time) (*NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updatedReservation, updatedOperation, err := s.memory.CompareAndSetReservationStatusWithOperation(ctx, reservationID, from, to, operation, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistLocked(ctx); err != nil {
		return nil, nil, err
	}
	return updatedReservation, updatedOperation, nil
}

func (s *DurableFileStore) CompareAndSetReservationStatusWithLease(ctx context.Context, reservationID string, leaseToken string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.CompareAndSetReservationStatusWithLease(ctx, reservationID, leaseToken, from, to, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) AcquireReservationLease(ctx context.Context, reservationID string, owner string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.AcquireReservationLease(ctx, reservationID, owner, leaseToken, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) AcquireReservationLeaseForStatus(ctx context.Context, reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.AcquireReservationLeaseForStatus(ctx, reservationID, owner, leaseToken, requiredStatus, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) HeartbeatReservationLease(ctx context.Context, reservationID string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.HeartbeatReservationLease(ctx, reservationID, leaseToken, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) HeartbeatReservationLeaseForStatus(ctx context.Context, reservationID string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.HeartbeatReservationLeaseForStatus(ctx, reservationID, leaseToken, requiredStatus, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) ClearReservationLease(ctx context.Context, reservationID string, leaseToken string, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.ClearReservationLease(ctx, reservationID, leaseToken, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) MarkReservationsProofReady(ctx context.Context, reservations []SubmittedReservationRef, operationUpdate ProofReadyOperationUpdate, now time.Time) ([]NoteReservation, *PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updatedReservations, updatedOperation, err := s.memory.MarkReservationsProofReady(ctx, reservations, operationUpdate, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistLocked(ctx); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperation, nil
}

func (s *DurableFileStore) MarkReservationSubmitted(ctx context.Context, reservationID string, leaseToken string, update SubmittedReservationUpdate, now time.Time) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.MarkReservationSubmitted(ctx, reservationID, leaseToken, update, now)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) MarkReservationsSubmitted(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update SubmittedReservationUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updatedReservations, updatedOperations, err := s.memory.MarkReservationsSubmitted(ctx, reservations, operationIDs, update, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistLocked(ctx); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperations, nil
}

func (s *DurableFileStore) MarkReservationsBroadcastUnknown(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updatedReservations, updatedOperations, err := s.memory.MarkReservationsBroadcastUnknown(ctx, reservations, operationIDs, update, now)
	if err != nil {
		return nil, nil, err
	}
	if err := s.persistLocked(ctx); err != nil {
		return nil, nil, err
	}
	return updatedReservations, updatedOperations, nil
}

func (s *DurableFileStore) UpdateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.UpdateReservation(ctx, reservation)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) CreateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	created, err := s.memory.CreateOperation(ctx, operation)
	if err != nil {
		return nil, err
	}
	return created, s.persistLocked(ctx)
}

func (s *DurableFileStore) GetOperation(ctx context.Context, operationID string) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memory.GetOperation(ctx, operationID)
}

func (s *DurableFileStore) UpdateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.memory.UpdateOperation(ctx, operation)
	if err != nil {
		return nil, err
	}
	return updated, s.persistLocked(ctx)
}

func (s *DurableFileStore) persist(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked(ctx)
}

func (s *DurableFileStore) persistLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("%w: durable reservation store path is required", ErrInvalidReservation)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil && filepath.Dir(s.path) != "." {
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
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

	return DurableFileStoreSnapshot{
		Version:      durableFileStoreVersion,
		UpdatedAt:    time.Now().UTC(),
		Reservations: reservations,
		Operations:   operations,
	}
}

func loadDurableFileStoreSnapshot(path string) (*MemoryStore, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewMemoryStore(), nil
		}
		return nil, err
	}
	var snapshot DurableFileStoreSnapshot
	decoder := json.NewDecoder(bytes.NewReader(bz))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, err
	}
	if snapshot.Version != durableFileStoreVersion {
		return nil, fmt.Errorf("%w: unsupported durable reservation store version %d", ErrInvalidReservation, snapshot.Version)
	}
	return memoryStoreFromSnapshot(snapshot)
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
	return store, nil
}
