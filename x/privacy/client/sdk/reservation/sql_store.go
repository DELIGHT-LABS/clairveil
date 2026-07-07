package reservation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SQLDialect string

const (
	SQLDialectPostgres SQLDialect = "postgres"
	SQLDialectSQLite   SQLDialect = "sqlite"
)

type SQLStore struct {
	DB      *sql.DB
	Dialect SQLDialect
}

var _ Store = (*SQLStore)(nil)

func InitSQLStore(ctx context.Context, db *sql.DB, dialect SQLDialect) error {
	if db == nil {
		return fmt.Errorf("sql db is required")
	}
	for _, stmt := range sqlSchemaStatements(dialect) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func PostgreSQLSchema() string {
	return strings.Join(sqlSchemaStatements(SQLDialectPostgres), ";\n\n") + ";\n"
}

func SQLiteSchema() string {
	return strings.Join(sqlSchemaStatements(SQLDialectSQLite), ";\n\n") + ";\n"
}

func (s *SQLStore) CreateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error) {
	created, err := s.CreateReservationBatch(ctx, []NoteReservation{reservation}, nil)
	if err != nil {
		return nil, err
	}
	return &created[0], nil
}

func (s *SQLStore) CreateReservationBatch(ctx context.Context, reservations []NoteReservation, operations []PayrollOperation) ([]NoteReservation, error) {
	var out []NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		created, err := memory.CreateReservationBatch(ctx, reservations, operations)
		if err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *SQLStore) GetReservation(ctx context.Context, reservationID string) (*NoteReservation, error) {
	memory, err := s.loadMemory(ctx)
	if err != nil {
		return nil, err
	}
	return memory.GetReservation(ctx, reservationID)
}

func (s *SQLStore) ListReservations(ctx context.Context, filter ReservationFilter) ([]NoteReservation, error) {
	memory, err := s.loadMemory(ctx)
	if err != nil {
		return nil, err
	}
	return memory.ListReservations(ctx, filter)
}

func (s *SQLStore) CompareAndSetReservationStatus(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	var out *NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.CompareAndSetReservationStatus(ctx, reservationID, from, to, now)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) CompareAndSetReservationStatusWithOperation(ctx context.Context, reservationID string, from ReservationStatus, to ReservationStatus, operation *PayrollOperation, now time.Time) (*NoteReservation, *PayrollOperation, error) {
	var outReservation *NoteReservation
	var outOperation *PayrollOperation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updatedReservation, updatedOperation, err := memory.CompareAndSetReservationStatusWithOperation(ctx, reservationID, from, to, operation, now)
		if err != nil {
			return err
		}
		outReservation = updatedReservation
		outOperation = updatedOperation
		return nil
	})
	return outReservation, outOperation, err
}

func (s *SQLStore) CompareAndSetReservationStatusWithLease(ctx context.Context, reservationID string, leaseToken string, from ReservationStatus, to ReservationStatus, now time.Time) (*NoteReservation, error) {
	var out *NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.CompareAndSetReservationStatusWithLease(ctx, reservationID, leaseToken, from, to, now)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) AcquireReservationLease(ctx context.Context, reservationID string, owner string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	var out *NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.AcquireReservationLease(ctx, reservationID, owner, leaseToken, leaseUntil, now)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) AcquireReservationLeaseForStatus(ctx context.Context, reservationID string, owner string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	var out *NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.AcquireReservationLeaseForStatus(ctx, reservationID, owner, leaseToken, requiredStatus, leaseUntil, now)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) HeartbeatReservationLease(ctx context.Context, reservationID string, leaseToken string, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	var out *NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.HeartbeatReservationLease(ctx, reservationID, leaseToken, leaseUntil, now)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) HeartbeatReservationLeaseForStatus(ctx context.Context, reservationID string, leaseToken string, requiredStatus ReservationStatus, leaseUntil time.Time, now time.Time) (*NoteReservation, error) {
	var out *NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.HeartbeatReservationLeaseForStatus(ctx, reservationID, leaseToken, requiredStatus, leaseUntil, now)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) ClearReservationLease(ctx context.Context, reservationID string, leaseToken string, now time.Time) (*NoteReservation, error) {
	var out *NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.ClearReservationLease(ctx, reservationID, leaseToken, now)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) MarkReservationsProofReady(ctx context.Context, reservations []SubmittedReservationRef, operationUpdate ProofReadyOperationUpdate, now time.Time) ([]NoteReservation, *PayrollOperation, error) {
	var outReservations []NoteReservation
	var outOperation *PayrollOperation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updatedReservations, updatedOperation, err := memory.MarkReservationsProofReady(ctx, reservations, operationUpdate, now)
		if err != nil {
			return err
		}
		outReservations = updatedReservations
		outOperation = updatedOperation
		return nil
	})
	return outReservations, outOperation, err
}

func (s *SQLStore) MarkReservationSubmitted(ctx context.Context, reservationID string, leaseToken string, update SubmittedReservationUpdate, now time.Time) (*NoteReservation, error) {
	var out *NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.MarkReservationSubmitted(ctx, reservationID, leaseToken, update, now)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) MarkReservationsSubmitted(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update SubmittedReservationUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	var outReservations []NoteReservation
	var outOperations []PayrollOperation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updatedReservations, updatedOperations, err := memory.MarkReservationsSubmitted(ctx, reservations, operationIDs, update, now)
		if err != nil {
			return err
		}
		outReservations = updatedReservations
		outOperations = updatedOperations
		return nil
	})
	return outReservations, outOperations, err
}

func (s *SQLStore) MarkReservationsBroadcastUnknown(ctx context.Context, reservations []SubmittedReservationRef, operationIDs []string, update BroadcastAttemptUpdate, now time.Time) ([]NoteReservation, []PayrollOperation, error) {
	var outReservations []NoteReservation
	var outOperations []PayrollOperation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updatedReservations, updatedOperations, err := memory.MarkReservationsBroadcastUnknown(ctx, reservations, operationIDs, update, now)
		if err != nil {
			return err
		}
		outReservations = updatedReservations
		outOperations = updatedOperations
		return nil
	})
	return outReservations, outOperations, err
}

func (s *SQLStore) UpdateReservation(ctx context.Context, reservation NoteReservation) (*NoteReservation, error) {
	var out *NoteReservation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.UpdateReservation(ctx, reservation)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) CreateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	var out *PayrollOperation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		created, err := memory.CreateOperation(ctx, operation)
		if err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *SQLStore) GetOperation(ctx context.Context, operationID string) (*PayrollOperation, error) {
	memory, err := s.loadMemory(ctx)
	if err != nil {
		return nil, err
	}
	return memory.GetOperation(ctx, operationID)
}

func (s *SQLStore) UpdateOperation(ctx context.Context, operation PayrollOperation) (*PayrollOperation, error) {
	var out *PayrollOperation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.UpdateOperation(ctx, operation)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) loadMemory(ctx context.Context) (*MemoryStore, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("sql db is required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	memory, loadErr := s.loadMemoryTx(ctx, tx)
	if loadErr != nil {
		_ = tx.Rollback()
		return nil, loadErr
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return memory, nil
}

func (s *SQLStore) withMemoryWrite(ctx context.Context, update func(*MemoryStore) error) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("sql db is required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	memory, err := s.loadMemoryTx(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := update(memory); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.persistMemoryTx(ctx, tx, memory); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) loadMemoryTx(ctx context.Context, tx *sql.Tx) (*MemoryStore, error) {
	memory := NewMemoryStore()
	reservationRows, err := tx.QueryContext(ctx, "SELECT payload_json FROM note_reservations")
	if err != nil {
		return nil, err
	}
	for reservationRows.Next() {
		var payload string
		if err := reservationRows.Scan(&payload); err != nil {
			_ = reservationRows.Close()
			return nil, err
		}
		var reservation NoteReservation
		if err := json.Unmarshal([]byte(payload), &reservation); err != nil {
			_ = reservationRows.Close()
			return nil, err
		}
		memory.storeReservationLocked(reservation)
	}
	if err := reservationRows.Close(); err != nil {
		return nil, err
	}
	if err := reservationRows.Err(); err != nil {
		return nil, err
	}

	operationRows, err := tx.QueryContext(ctx, "SELECT payload_json FROM payroll_operations")
	if err != nil {
		return nil, err
	}
	for operationRows.Next() {
		var payload string
		if err := operationRows.Scan(&payload); err != nil {
			_ = operationRows.Close()
			return nil, err
		}
		var operation PayrollOperation
		if err := json.Unmarshal([]byte(payload), &operation); err != nil {
			_ = operationRows.Close()
			return nil, err
		}
		if strings.TrimSpace(operation.OperationID) == "" {
			_ = operationRows.Close()
			return nil, fmt.Errorf("%w: operation_id is required", ErrInvalidReservation)
		}
		memory.operations[operation.OperationID] = cloneOperation(operation)
	}
	if err := operationRows.Close(); err != nil {
		return nil, err
	}
	if err := operationRows.Err(); err != nil {
		return nil, err
	}
	return memory, nil
}

func (s *SQLStore) persistMemoryTx(ctx context.Context, tx *sql.Tx, memory *MemoryStore) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM note_reservations"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM payroll_operations"); err != nil {
		return err
	}

	operations := make([]PayrollOperation, 0, len(memory.operations))
	for _, operation := range memory.operations {
		operations = append(operations, cloneOperation(operation))
	}
	sort.Slice(operations, func(i, j int) bool {
		return operations[i].OperationID < operations[j].OperationID
	})
	for _, operation := range operations {
		payload, err := json.Marshal(operation)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, s.insertOperationSQL(), operation.OperationID, string(operation.Status), string(payload), operation.UpdatedAt); err != nil {
			return err
		}
	}

	reservations := make([]NoteReservation, 0, len(memory.reservations))
	for _, reservation := range memory.reservations {
		reservations = append(reservations, cloneReservation(reservation))
	}
	sort.Slice(reservations, func(i, j int) bool {
		return reservations[i].ReservationID < reservations[j].ReservationID
	})
	for _, reservation := range reservations {
		payload, err := json.Marshal(reservation)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, s.insertReservationSQL(),
			reservation.ReservationID,
			reservation.OwnerKeyID,
			reservation.NullifierLookupKey,
			string(reservation.Status),
			reservation.OperationID,
			string(payload),
			reservation.UpdatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) insertReservationSQL() string {
	p := sqlPlaceholderer(s.Dialect)
	return fmt.Sprintf("INSERT INTO note_reservations (reservation_id, owner_key_id, nullifier_lookup_key, status, operation_id, payload_json, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s)",
		p(1), p(2), p(3), p(4), p(5), p(6), p(7))
}

func (s *SQLStore) insertOperationSQL() string {
	p := sqlPlaceholderer(s.Dialect)
	return fmt.Sprintf("INSERT INTO payroll_operations (operation_id, status, payload_json, updated_at) VALUES (%s, %s, %s, %s)",
		p(1), p(2), p(3), p(4))
}

func sqlPlaceholderer(dialect SQLDialect) func(int) string {
	if dialect == SQLDialectPostgres {
		return func(i int) string { return "$" + fmt.Sprint(i) }
	}
	return func(int) string { return "?" }
}

func sqlSchemaStatements(dialect SQLDialect) []string {
	timestampType := "TIMESTAMPTZ"
	if dialect == SQLDialectSQLite {
		timestampType = "TEXT"
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS payroll_operations (
  operation_id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  updated_at %s
)`, timestampType),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS note_reservations (
  reservation_id TEXT PRIMARY KEY,
  owner_key_id TEXT NOT NULL,
  nullifier_lookup_key TEXT NOT NULL,
  status TEXT NOT NULL,
  operation_id TEXT,
  payload_json TEXT NOT NULL,
  updated_at %s
)`, timestampType),
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_active_note_reservation
ON note_reservations(owner_key_id, nullifier_lookup_key)
WHERE status IN ('Reserved', 'Proving', 'ProofReady', 'Submitted', 'Unknown', 'ManualReview')`,
		`CREATE INDEX IF NOT EXISTS idx_note_reservations_status
ON note_reservations(status)`,
		`CREATE INDEX IF NOT EXISTS idx_note_reservations_operation
ON note_reservations(operation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_payroll_operations_status
ON payroll_operations(status)`,
	}
}
