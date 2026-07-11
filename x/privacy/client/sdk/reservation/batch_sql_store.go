package reservation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

var _ BatchOperationStore = (*SQLStore)(nil)

func (s *SQLStore) CreateBatchOperation(ctx context.Context, reservations []NoteReservation, graph BatchOperationGraph) (*BatchOperationGraph, error) {
	var out *BatchOperationGraph
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		created, err := memory.CreateBatchOperation(ctx, reservations, graph)
		if err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *SQLStore) GetBatchOperation(ctx context.Context, operationID string) (*BatchOperationGraph, error) {
	memory, err := s.loadMemory(ctx)
	if err != nil {
		return nil, err
	}
	return memory.GetBatchOperation(ctx, operationID)
}

func (s *SQLStore) AcquireBatchOperationLease(ctx context.Context, operationID, owner, token string, leaseUntil, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func(memory *MemoryStore) (*BatchOperation, error) {
		return memory.AcquireBatchOperationLease(ctx, operationID, owner, token, leaseUntil, now)
	})
}

func (s *SQLStore) HeartbeatBatchOperationLease(ctx context.Context, operationID, token string, leaseUntil, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func(memory *MemoryStore) (*BatchOperation, error) {
		return memory.HeartbeatBatchOperationLease(ctx, operationID, token, leaseUntil, now)
	})
}

func (s *SQLStore) ReleaseBatchOperationLease(ctx context.Context, operationID, token string, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func(memory *MemoryStore) (*BatchOperation, error) {
		return memory.ReleaseBatchOperationLease(ctx, operationID, token, now)
	})
}

func (s *SQLStore) CompareAndSetBatchOperationStatus(ctx context.Context, operationID, leaseToken string, from, to OperationStatus, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func(memory *MemoryStore) (*BatchOperation, error) {
		return memory.CompareAndSetBatchOperationStatus(ctx, operationID, leaseToken, from, to, now)
	})
}

func (s *SQLStore) SaveBatchProofArtifacts(ctx context.Context, operationID, leaseToken string, update BatchProofArtifactUpdate, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func(memory *MemoryStore) (*BatchOperation, error) {
		return memory.SaveBatchProofArtifacts(ctx, operationID, leaseToken, update, now)
	})
}

func (s *SQLStore) SaveBatchSignedTx(ctx context.Context, operationID, leaseToken string, update BatchSignedTxUpdate, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func(memory *MemoryStore) (*BatchOperation, error) {
		return memory.SaveBatchSignedTx(ctx, operationID, leaseToken, update, now)
	})
}

func (s *SQLStore) RecordBatchBroadcast(ctx context.Context, operationID, leaseToken string, update BatchBroadcastUpdate, now time.Time) (*BatchOperation, error) {
	return s.mutateBatchOperation(ctx, func(memory *MemoryStore) (*BatchOperation, error) {
		return memory.RecordBatchBroadcast(ctx, operationID, leaseToken, update, now)
	})
}

func (s *SQLStore) ReconcileBatchOperation(ctx context.Context, operationID string, update BatchReconcileUpdate, now time.Time) (*BatchOperationGraph, error) {
	var out *BatchOperationGraph
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := memory.ReconcileBatchOperation(ctx, operationID, update, now)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) mutateBatchOperation(ctx context.Context, mutate func(*MemoryStore) (*BatchOperation, error)) (*BatchOperation, error) {
	var out *BatchOperation
	err := s.withMemoryWrite(ctx, func(memory *MemoryStore) error {
		updated, err := mutate(memory)
		if err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *SQLStore) loadBatchRelationsTx(ctx context.Context, tx *sql.Tx, memory *MemoryStore) error {
	if err := loadJSONRows(ctx, tx, "SELECT payload_json FROM batch_operations", func(payload []byte) error {
		var operation BatchOperation
		if err := json.Unmarshal(payload, &operation); err != nil {
			return err
		}
		if operation.SchemaVersion != BatchOperationSchemaVersionV1 || strings.TrimSpace(operation.OperationID) == "" {
			return fmt.Errorf("%w: invalid batch operation SQL row", ErrInvalidReservation)
		}
		if _, duplicate := memory.batchOperations[operation.OperationID]; duplicate {
			return fmt.Errorf("%w: duplicate batch operation SQL row", ErrInvalidReservation)
		}
		memory.batchOperations[operation.OperationID] = cloneBatchOperation(operation)
		return nil
	}); err != nil {
		return err
	}
	if err := loadJSONRows(ctx, tx, "SELECT payload_json FROM batch_operation_inputs", func(payload []byte) error {
		var input OperationInputReservation
		if err := json.Unmarshal(payload, &input); err != nil {
			return err
		}
		memory.batchInputs[input.OperationID] = append(memory.batchInputs[input.OperationID], input)
		return nil
	}); err != nil {
		return err
	}
	if err := loadJSONRows(ctx, tx, "SELECT payload_json FROM payroll_item_outputs", func(payload []byte) error {
		var item PayrollItemOutput
		if err := json.Unmarshal(payload, &item); err != nil {
			return err
		}
		memory.batchItems[item.OperationID] = append(memory.batchItems[item.OperationID], item)
		return nil
	}); err != nil {
		return err
	}
	if err := loadJSONRows(ctx, tx, "SELECT payload_json FROM expected_output_evidence", func(payload []byte) error {
		var evidence ExpectedOutputEvidence
		if err := json.Unmarshal(payload, &evidence); err != nil {
			return err
		}
		memory.batchEvidence[evidence.OperationID] = append(memory.batchEvidence[evidence.OperationID], evidence)
		return nil
	}); err != nil {
		return err
	}
	return memory.validatePersistedBatchGraphsLocked()
}

func loadJSONRows(ctx context.Context, tx *sql.Tx, query string, consume func([]byte) error) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return err
		}
		if err := consume([]byte(payload)); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *SQLStore) persistBatchRelationsTx(ctx context.Context, tx *sql.Tx, memory *MemoryStore) error {
	operationIDs := make([]string, 0, len(memory.batchOperations))
	for operationID := range memory.batchOperations {
		operationIDs = append(operationIDs, operationID)
	}
	sort.Strings(operationIDs)
	for _, operationID := range operationIDs {
		op := cloneBatchOperation(memory.batchOperations[operationID])
		payload, err := json.Marshal(op)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, s.insertBatchOperationSQL(), op.OperationID, string(op.Status), string(payload), op.UpdatedAt); err != nil {
			return err
		}
		for _, input := range cloneBatchInputs(memory.batchInputs[operationID]) {
			payload, err := json.Marshal(input)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, s.insertBatchInputSQL(), input.OperationID, input.InputIndex, input.ReservationID, string(payload)); err != nil {
				return err
			}
		}
		for _, item := range cloneBatchItems(memory.batchItems[operationID]) {
			payload, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, s.insertBatchItemSQL(), item.OperationID, item.OutputIndex, nullableString(item.ItemID), string(item.EvidenceStatus), string(payload), item.UpdatedAt); err != nil {
				return err
			}
		}
		for _, evidence := range cloneBatchEvidence(memory.batchEvidence[operationID]) {
			payload, err := json.Marshal(evidence)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, s.insertBatchEvidenceSQL(), evidence.OperationID, evidence.OutputIndex, string(payload), evidence.UpdatedAt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLStore) insertBatchOperationSQL() string {
	p := sqlPlaceholderer(s.Dialect)
	return fmt.Sprintf("INSERT INTO batch_operations (operation_id, status, payload_json, updated_at) VALUES (%s, %s, %s, %s)", p(1), p(2), p(3), p(4))
}

func (s *SQLStore) insertBatchInputSQL() string {
	p := sqlPlaceholderer(s.Dialect)
	return fmt.Sprintf("INSERT INTO batch_operation_inputs (operation_id, input_index, reservation_id, payload_json) VALUES (%s, %s, %s, %s)", p(1), p(2), p(3), p(4))
}

func (s *SQLStore) insertBatchItemSQL() string {
	p := sqlPlaceholderer(s.Dialect)
	return fmt.Sprintf("INSERT INTO payroll_item_outputs (operation_id, output_index, item_id, evidence_status, payload_json, updated_at) VALUES (%s, %s, %s, %s, %s, %s)", p(1), p(2), p(3), p(4), p(5), p(6))
}

func (s *SQLStore) insertBatchEvidenceSQL() string {
	p := sqlPlaceholderer(s.Dialect)
	return fmt.Sprintf("INSERT INTO expected_output_evidence (operation_id, output_index, payload_json, updated_at) VALUES (%s, %s, %s, %s)", p(1), p(2), p(3), p(4))
}

func nullableString(value string) driver.Value {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
