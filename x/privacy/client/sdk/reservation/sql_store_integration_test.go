package reservation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const postgresIntegrationDSNEnv = "CLAIRVEIL_TEST_POSTGRES_DSN"

func TestSQLStoreSQLiteIntegrationGraphAtomicityAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reservations.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)", path)
	runSQLStoreIntegration(t, SQLDialectSQLite, func(t *testing.T) *sql.DB {
		db, err := sql.Open("sqlite", dsn)
		require.NoError(t, err)
		db.SetMaxOpenConns(4)
		require.NoError(t, db.PingContext(context.Background()))
		return db
	})
}

func TestInitSQLStoreRejectsLegacyLifecycleSchemaWithoutMigratingIt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-reservations.db")
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s", path))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(ctx))

	_, err = db.ExecContext(ctx, `
		CREATE TABLE reservation_lifecycle_store_meta (
			singleton_id INTEGER PRIMARY KEY,
			schema_version INTEGER NOT NULL
		)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO reservation_lifecycle_store_meta (singleton_id, schema_version)
		VALUES (1, 1)`)
	require.NoError(t, err)

	err = InitSQLStore(ctx, db, SQLDialectSQLite)
	require.ErrorIs(t, err, ErrInvalidReservation)
	require.ErrorContains(t, err, "unsupported reservation lifecycle SQL schema version 1")

	var version int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT schema_version FROM reservation_lifecycle_store_meta WHERE singleton_id = 1",
	).Scan(&version))
	require.Equal(t, 1, version)

	var currentSchemaTables int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'note_reservations'`,
	).Scan(&currentSchemaTables))
	require.Zero(t, currentSchemaTables, "rejected v1 initialization must roll back current-schema DDL")
}

func TestSQLStorePostgreSQLIntegrationGraphAtomicityAndRecovery(t *testing.T) {
	dsn := os.Getenv(postgresIntegrationDSNEnv)
	if dsn == "" {
		t.Skip(postgresIntegrationDSNEnv + " is required for the live PostgreSQL integration test")
	}
	runSQLStoreIntegration(t, SQLDialectPostgres, func(t *testing.T) *sql.DB {
		db, err := sql.Open("postgres", dsn)
		require.NoError(t, err)
		db.SetMaxOpenConns(4)
		require.NoError(t, db.PingContext(context.Background()))
		return db
	})
}

func runSQLStoreIntegration(t *testing.T, dialect SQLDialect, open func(*testing.T) *sql.DB) {
	t.Helper()
	ctx := context.Background()
	db := open(t)
	require.NoError(t, resetSQLIntegrationSchema(ctx, db, dialect))
	t.Cleanup(func() {
		if db == nil {
			return
		}
		if err := resetSQLIntegrationSchema(ctx, db, dialect); err != nil {
			t.Errorf("reset SQL integration schema: %v", err)
		}
		_ = db.Close()
	})
	require.NoError(t, InitSQLStore(ctx, db, dialect))
	store := &SQLStore{DB: db, Dialect: dialect}

	reservations, graph := testBatchOperationGraph("sql-integration-op", 3, 4)
	created, err := store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)
	require.Len(t, created.Inputs, 3)
	require.Len(t, created.Items, 4)
	require.Len(t, created.Evidence, 4)
	assertSQLGraphRowCounts(t, db, 3, 1, 3, 4, 4)

	require.NoError(t, installSQLPersistFailureTrigger(ctx, db, dialect))
	_, err = store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "rollback-worker", "rollback-token", fixedNow().Add(time.Minute), fixedNow())
	require.ErrorContains(t, err, "forced batch operation persist failure")
	require.NoError(t, removeSQLPersistFailureTrigger(ctx, db, dialect))
	rolledBack, err := store.GetBatchOperation(ctx, graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, OperationStatusPlanned, rolledBack.Operation.Status)
	require.Empty(t, rolledBack.Operation.LeaseToken)
	assertSQLGraphRowCounts(t, db, 3, 1, 3, 4, 4)

	require.NoError(t, db.Close())
	db = open(t)
	store = &SQLStore{DB: db, Dialect: dialect}
	reopened, err := store.GetBatchOperation(ctx, graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, graph.Operation.OperationID, reopened.Operation.OperationID)
	require.Len(t, reopened.Inputs, 3)
	require.Len(t, reopened.Items, 4)
	require.Len(t, reopened.Evidence, 4)
	for i := range reopened.Inputs {
		require.Equal(t, i, reopened.Inputs[i].InputIndex)
	}
	for i := range reopened.Items {
		require.Equal(t, i, reopened.Items[i].OutputIndex)
		require.Equal(t, i, reopened.Evidence[i].OutputIndex)
	}

	conflict := testReservation("ordinary-conflict", "ordinary-note", "ordinary-operation")
	conflict.OwnerKeyID = reservations[0].OwnerKeyID
	conflict.NullifierLookupKey = reservations[0].NullifierLookupKey
	_, err = store.CreateReservation(ctx, conflict)
	require.ErrorIs(t, err, ErrActiveReservationExists)
	assertSQLGraphRowCounts(t, db, 3, 1, 3, 4, 4)

	concurrent := []NoteReservation{
		testReservation("concurrent-reservation-a", "concurrent-note-a", ""),
		testReservation("concurrent-reservation-b", "concurrent-note-b", ""),
	}
	for i := range concurrent {
		concurrent[i].OwnerKeyID = "concurrent-owner"
		concurrent[i].NullifierLookupKey = "concurrent-nullifier"
		concurrent[i].CreatedAt = fixedNow()
		concurrent[i].UpdatedAt = fixedNow()
	}
	start := make(chan struct{})
	results := make(chan error, len(concurrent))
	var workers sync.WaitGroup
	for i := range concurrent {
		reservation := concurrent[i]
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, createErr := store.CreateReservation(ctx, reservation)
			results <- createErr
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for createErr := range results {
		switch {
		case createErr == nil:
			succeeded++
		case errors.Is(createErr, ErrActiveReservationExists):
			conflicted++
		default:
			t.Fatalf("unexpected concurrent reservation result: %v", createErr)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicted)
	assertSQLGraphRowCounts(t, db, 4, 1, 3, 4, 4)

	now := fixedNow().Add(10 * time.Minute)
	first, err := store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "proof-worker-a", "lease-a", now.Add(time.Minute), now)
	require.NoError(t, err)
	require.Equal(t, "lease-a", first.LeaseToken)
	heartbeat, err := store.HeartbeatBatchOperationLease(ctx, graph.Operation.OperationID, first.LeaseToken, now.Add(3*time.Minute), now.Add(30*time.Second))
	require.NoError(t, err)
	require.Equal(t, now.Add(3*time.Minute), heartbeat.LeaseUntil)
	for _, reservation := range reservations {
		stored, getErr := store.GetReservation(ctx, reservation.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, first.LeaseToken, stored.LeaseToken)
		require.Equal(t, heartbeat.LastHeartbeatAt, stored.LastHeartbeatAt)
		require.Equal(t, heartbeat.LeaseUntil, stored.LeaseUntil)
	}
	_, err = store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "proof-worker-b", "lease-b", now.Add(2*time.Minute), now.Add(90*time.Second))
	require.ErrorIs(t, err, ErrLeaseUnavailable)
	second, err := store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "proof-worker-b", "lease-b", now.Add(6*time.Minute), now.Add(4*time.Minute))
	require.NoError(t, err)
	require.Equal(t, "lease-b", second.LeaseToken)
	for _, reservation := range reservations {
		stored, getErr := store.GetReservation(ctx, reservation.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, second.LeaseToken, stored.LeaseToken)
		require.Equal(t, second.LeaseUntil, stored.LeaseUntil)
	}

	_, err = store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, "lease-a", OperationStatusPlanned, OperationStatusProving, now.Add(4*time.Minute))
	require.ErrorIs(t, err, ErrLeaseMismatch)
	_, err = store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, second.LeaseToken, OperationStatusProofReady, OperationStatusProving, now.Add(4*time.Minute))
	require.ErrorIs(t, err, ErrCompareAndSetFailed)
	proving, err := store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, second.LeaseToken, OperationStatusPlanned, OperationStatusProving, now.Add(4*time.Minute))
	require.NoError(t, err)
	require.Equal(t, OperationStatusProving, proving.Status)

	observed := make([]ObservedOutputEvidence, len(graph.Evidence))
	for i, expected := range graph.Evidence {
		observed[i] = ObservedOutputEvidence{
			OutputIndex: expected.OutputIndex, Commitment: expected.Commitment,
			UserDisclosureDigest: expected.UserDisclosureDigest, FullDisclosureDigest: expected.FullDisclosureDigest,
			RecipientHash: expected.RecipientHash,
		}
	}
	spentIDs := make([]string, len(reservations))
	for i := range reservations {
		spentIDs[i] = reservations[i].ReservationID
	}
	reconciled, err := store.ReconcileBatchOperation(ctx, graph.Operation.OperationID, BatchReconcileUpdate{
		TxHash: "sql-live-tx", TxSucceeded: true, SpentReservationIDs: spentIDs, ObservedOutputs: observed,
	}, now.Add(5*time.Minute))
	require.NoError(t, err)
	require.Equal(t, OperationStatusSucceeded, reconciled.Operation.Status)
	for _, item := range reconciled.Items {
		require.Equal(t, BatchItemEvidenceSucceeded, item.EvidenceStatus)
	}

	require.NoError(t, db.Close())
	db = open(t)
	store = &SQLStore{DB: db, Dialect: dialect}
	final, err := store.GetBatchOperation(ctx, graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, OperationStatusSucceeded, final.Operation.Status)
	require.Equal(t, "sql-live-tx", final.Operation.TxHash)
	for _, item := range final.Items {
		require.Equal(t, BatchItemEvidenceSucceeded, item.EvidenceStatus)
	}
	for _, reservation := range reservations {
		stored, getErr := store.GetReservation(ctx, reservation.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, StatusConfirmedSpent, stored.Status)
	}
	assertSQLGraphRowCounts(t, db, 4, 1, 3, 4, 4)
}

func assertSQLGraphRowCounts(t *testing.T, db *sql.DB, reservations, operations, inputs, items, evidence int) {
	t.Helper()
	for table, expected := range map[string]int{
		"note_reservations":        reservations,
		"batch_operations":         operations,
		"batch_operation_inputs":   inputs,
		"payroll_item_outputs":     items,
		"expected_output_evidence": evidence,
	} {
		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
		require.Equal(t, expected, count, table)
	}
}

func installSQLPersistFailureTrigger(ctx context.Context, db *sql.DB, dialect SQLDialect) error {
	if dialect == SQLDialectPostgres {
		if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION clairveil_test_fail_batch_insert() RETURNS trigger AS $body$
BEGIN
  RAISE EXCEPTION 'forced batch operation persist failure';
END;
$body$ LANGUAGE plpgsql`); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, `CREATE TRIGGER clairveil_test_fail_batch_insert
BEFORE INSERT ON batch_operations
FOR EACH ROW EXECUTE FUNCTION clairveil_test_fail_batch_insert()`)
		return err
	}
	_, err := db.ExecContext(ctx, `CREATE TRIGGER clairveil_test_fail_batch_insert
BEFORE INSERT ON batch_operations
BEGIN
  SELECT RAISE(ABORT, 'forced batch operation persist failure');
END`)
	return err
}

func removeSQLPersistFailureTrigger(ctx context.Context, db *sql.DB, dialect SQLDialect) error {
	if dialect == SQLDialectPostgres {
		if _, err := db.ExecContext(ctx, "DROP TRIGGER IF EXISTS clairveil_test_fail_batch_insert ON batch_operations"); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, "DROP FUNCTION IF EXISTS clairveil_test_fail_batch_insert()")
		return err
	}
	_, err := db.ExecContext(ctx, "DROP TRIGGER IF EXISTS clairveil_test_fail_batch_insert")
	return err
}

func resetSQLIntegrationSchema(ctx context.Context, db *sql.DB, dialect SQLDialect) error {
	tables := []string{
		"expected_output_evidence", "payroll_item_outputs", "batch_operation_inputs", "batch_operations",
		"note_reservations", "payroll_operations", "reservation_lifecycle_store_meta", "batch_operation_store_meta", "reservation_store_locks",
	}
	for _, table := range tables {
		statement := "DROP TABLE IF EXISTS " + table
		if dialect == SQLDialectPostgres {
			statement += " CASCADE"
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if dialect == SQLDialectPostgres {
		_, err := db.ExecContext(ctx, "DROP FUNCTION IF EXISTS clairveil_test_fail_batch_insert()")
		return err
	}
	return nil
}
