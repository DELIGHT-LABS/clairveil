package reservation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLStoreSatisfiesStoreInterface(t *testing.T) {
	var _ Store = (*SQLStore)(nil)
}

func TestPostgreSQLSchemaIncludesActiveReservationConstraint(t *testing.T) {
	schema := PostgreSQLSchema()
	require.Contains(t, schema, "CREATE UNIQUE INDEX IF NOT EXISTS uniq_active_note_reservation")
	require.Contains(t, schema, "CREATE TABLE IF NOT EXISTS reservation_store_locks")
	require.Contains(t, schema, "owner_key_id, nullifier_lookup_key")
	require.Contains(t, schema, "'Reserved', 'Proving', 'ProofReady', 'Submitted', 'Unknown', 'ManualReview'")
	require.Contains(t, schema, "TIMESTAMPTZ")
}

func TestSQLiteSchemaUsesTextTimestamps(t *testing.T) {
	schema := SQLiteSchema()
	require.Contains(t, schema, "updated_at TEXT")
	require.NotContains(t, schema, "TIMESTAMPTZ")
}

func TestSQLStoreUsesDialectPlaceholders(t *testing.T) {
	postgres := (&SQLStore{Dialect: SQLDialectPostgres}).insertReservationSQL()
	require.Contains(t, postgres, "$1")
	require.Contains(t, postgres, "$7")

	sqlite := (&SQLStore{Dialect: SQLDialectSQLite}).insertReservationSQL()
	require.Equal(t, 7, strings.Count(sqlite, "?"))
	require.NotContains(t, sqlite, "$1")
}
