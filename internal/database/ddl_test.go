package database

import (
	"context"
	"os"
	"testing"
)

// TestSyncDDL verifies the last_synced marker table is created and seeded on a
// fresh database and that SyncDDL is idempotent across runs. Runs only when
// TEST_DATABASE_URL is set, mirroring the other integration tests.
func TestSyncDDL(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()

	db, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	// Ensure the table is created fresh so the seed assertion is meaningful.
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS last_synced`); err != nil {
		t.Fatalf("drop last_synced: %v", err)
	}

	if err := SyncDDL(ctx, pool); err != nil {
		t.Fatalf("SyncDDL (first run): %v", err)
	}

	var syncedAt int64
	if err := db.QueryRowContext(ctx, `SELECT synced_at FROM last_synced WHERE id = 1`).Scan(&syncedAt); err != nil {
		t.Fatalf("read last_synced: %v", err)
	}
	if syncedAt != initialLastSynced {
		t.Fatalf("synced_at = %d, want baseline %d", syncedAt, initialLastSynced)
	}

	// A second run must be a no-op, not an error.
	if err := SyncDDL(ctx, pool); err != nil {
		t.Fatalf("SyncDDL (second run, should be idempotent): %v", err)
	}
	var again int64
	if err := db.QueryRowContext(ctx, `SELECT synced_at FROM last_synced WHERE id = 1`).Scan(&again); err != nil {
		t.Fatalf("re-read last_synced: %v", err)
	}
	if again != syncedAt {
		t.Fatalf("synced_at changed across idempotent runs: %d -> %d", syncedAt, again)
	}
}
