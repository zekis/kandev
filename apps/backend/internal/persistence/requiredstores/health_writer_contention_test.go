package requiredstores

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/db"
	"github.com/stretchr/testify/require"
)

// The catalog phase must not requeue behind application writes after the
// writer health check has succeeded. WAL readers can still verify the schema.
func TestHealthCatalogDoesNotReacquireBusyWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "health.db")
	w, err := db.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	r, err := db.OpenSQLiteReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	writer, reader := sqlx.NewDb(w, "sqlite3"), sqlx.NewDb(r, "sqlite3")
	health := seedBatchHealth(t, db.NewPool(writer, reader))
	_, err = health.pingObserved(context.Background())
	require.NoError(t, err)
	tx, err := writer.BeginTxx(context.Background(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`INSERT INTO shared VALUES ('uncommitted')`)
	require.NoError(t, err)
	before := writer.Stats()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	results, diagnostic := health.probeSQLiteCatalog(ctx)
	require.Nil(t, diagnostic)
	for _, err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, before.WaitCount, writer.Stats().WaitCount, "schema reads must not queue on writer")
	// Full probes still require writer availability and fail closed.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	require.ErrorIs(t, health.Check(ctx2), context.DeadlineExceeded)
	require.False(t, health.Healthy())
	require.NoError(t, tx.Rollback())
	require.NoError(t, health.Check(context.Background()))
	_, err = writer.Exec(`DROP TABLE extra`)
	require.NoError(t, err)
	require.Error(t, health.Check(context.Background()))
	require.Equal(t, []string{"b"}, health.UnhealthyStoreIDs())
}

func openHealthContentionPools(t *testing.T) (*sqlx.DB, *sqlx.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contention.db")
	writer, err := db.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	reader, err := db.OpenSQLiteReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	reader.SetMaxOpenConns(1)
	return sqlx.NewDb(writer, "sqlite3"), sqlx.NewDb(reader, "sqlite3")
}

func TestHealthCorruptDatabaseFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("not a database", 100)), 0600))
	writer, err := db.OpenSQLite(path)
	require.NoError(t, err)
	defer func() { _ = writer.Close() }()
	reader, err := db.OpenSQLiteReader(path)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	tracker, err := NewTracker(batchDescriptors())
	require.NoError(t, err)
	for _, descriptor := range batchDescriptors() {
		require.NoError(t, tracker.RecordSuccess(descriptor.ID))
	}
	health := NewHealth(tracker, db.NewPool(sqlx.NewDb(writer, "sqlite3"), sqlx.NewDb(reader, "sqlite3")), nil)
	require.Error(t, health.Check(context.Background()))
	require.False(t, health.Healthy())
	require.Equal(t, []string{"a", "b", "c"}, health.UnhealthyStoreIDs())
}
