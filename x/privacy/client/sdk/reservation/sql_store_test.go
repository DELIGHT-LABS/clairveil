package reservation

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLStoreSatisfiesStoreInterface(t *testing.T) {
	var _ Store = (*SQLStore)(nil)
}

func TestConcreteStoresDoNotExposeUnsafeLifecycleMutators(t *testing.T) {
	stores := []any{NewMemoryStore(), &DurableFileStore{}, &SQLStore{}}
	for _, store := range stores {
		storeType := reflect.TypeOf(store)
		for _, method := range []string{
			"UpdateReservation",
			"UpdateOperation",
			"CompareAndSetReservationStatusWithOperation",
			"UnsafeImportReservationForTesting",
		} {
			if _, exposed := storeType.MethodByName(method); exposed {
				t.Fatalf("%s must not expose %s", storeType, method)
			}
		}
	}
}

func TestPostgreSQLSchemaIncludesActiveReservationConstraint(t *testing.T) {
	schema := PostgreSQLSchema()
	require.Contains(t, schema, "CREATE UNIQUE INDEX IF NOT EXISTS uniq_active_note_reservation")
	require.Contains(t, schema, "CREATE TABLE IF NOT EXISTS reservation_store_locks")
	require.Contains(t, schema, "INSERT INTO reservation_store_locks")
	require.Contains(t, schema, "INSERT INTO batch_operation_store_meta")
	require.Contains(t, schema, "INSERT INTO reservation_lifecycle_store_meta")
	require.Contains(t, schema, fmt.Sprint(LifecycleSchemaVersionV2))
	require.Contains(t, schema, BatchOperationSchemaVersionV1)
	require.Contains(t, schema, "ON CONFLICT (singleton_id) DO NOTHING")
	require.Contains(t, schema, "owner_key_id, nullifier_lookup_key")
	require.Contains(t, schema, "'Reserved', 'Proving', 'ProofReady', 'Submitted', 'Unknown', 'ManualReview'")
	require.Contains(t, schema, "TIMESTAMPTZ")
}

func TestSQLiteSchemaUsesTextTimestamps(t *testing.T) {
	schema := SQLiteSchema()
	require.Contains(t, schema, "updated_at TEXT")
	require.NotContains(t, schema, "TIMESTAMPTZ")
	require.Contains(t, schema, "INSERT INTO reservation_store_locks")
	require.Contains(t, schema, "INSERT INTO batch_operation_store_meta")
	require.Contains(t, schema, "INSERT INTO reservation_lifecycle_store_meta")
	require.Contains(t, schema, fmt.Sprint(LifecycleSchemaVersionV2))
	require.Contains(t, schema, BatchOperationSchemaVersionV1)
}

func TestSQLStoreUsesDialectPlaceholders(t *testing.T) {
	postgres := (&SQLStore{Dialect: SQLDialectPostgres}).insertReservationSQL()
	require.Contains(t, postgres, "$1")
	require.Contains(t, postgres, "$7")

	sqlite := (&SQLStore{Dialect: SQLDialectSQLite}).insertReservationSQL()
	require.Equal(t, 7, strings.Count(sqlite, "?"))
	require.NotContains(t, sqlite, "$1")
}

func TestSQLStorePostgresReadsUseRepeatableSnapshot(t *testing.T) {
	postgres := (&SQLStore{Dialect: SQLDialectPostgres}).readTxOptions()
	if !postgres.ReadOnly || postgres.Isolation != sql.LevelRepeatableRead {
		t.Fatalf("expected PostgreSQL repeatable-read snapshot, got %+v", postgres)
	}
	sqlite := (&SQLStore{Dialect: SQLDialectSQLite}).readTxOptions()
	if !sqlite.ReadOnly || sqlite.Isolation != sql.LevelDefault {
		t.Fatalf("expected SQLite read-only default isolation, got %+v", sqlite)
	}
}
