package database

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed ddl/*.sql
var ddlFS embed.FS

// initialLastSynced is the starting last_synced marker (DDMMYYYYHHMM):
// 18 Aug 2026, 01:36. Any DDL script with a timestamp at or below this value
// is treated as already applied, so the folder can ship historical scripts
// (e.g. 180820260133_create_users.sql) without re-running them on a fresh DB.
const initialLastSynced int64 = 180820260136

// SyncDDL keeps the database in sync with the timestamped SQL scripts in the
// embedded ddl/ folder ("sync on the fly"):
//
//  1. ensures the last_synced marker table exists (seeded to initialLastSynced
//     on first boot),
//  2. lists ddl/*.sql, sorted by name,
//  3. runs every script whose timestamp is NEWER than the current marker, each
//     in its own transaction, advancing the marker as it goes.
//
// Safe to run on every boot; concurrent runners are guarded with an advisory
// lock. Scripts must be named <DDMMYYYYHHMM>_<description>.sql.
func SyncDDL(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS last_synced (
			id         SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			synced_at  BIGINT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("ensure last_synced table: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO last_synced (id, synced_at) VALUES (1, $1) ON CONFLICT (id) DO NOTHING`,
		initialLastSynced); err != nil {
		return fmt.Errorf("seed last_synced: %w", err)
	}

	// Serialize concurrent bootstraps (e.g. multiple replicas starting at once).
	if _, err := pool.Exec(ctx, "SELECT pg_advisory_lock(747002)"); err != nil {
		return fmt.Errorf("acquire ddl sync lock: %w", err)
	}
	defer pool.Exec(context.Background(), "SELECT pg_advisory_unlock(747002)") //nolint:errcheck

	var syncedAt int64
	if err := pool.QueryRow(ctx, `SELECT synced_at FROM last_synced WHERE id = 1`).Scan(&syncedAt); err != nil {
		return fmt.Errorf("read last_synced: %w", err)
	}

	entries, err := ddlFS.ReadDir("ddl")
	if err != nil {
		return fmt.Errorf("read ddl folder: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	log := slog.Default()
	for _, name := range names {
		if !strings.HasSuffix(name, ".sql") {
			continue // ignore non-SQL files (README, etc.)
		}
		ts, err := ddlTimestamp(name)
		if err != nil {
			return fmt.Errorf("invalid ddl script name %q: %w", name, err)
		}
		if ts <= syncedAt {
			continue // already applied (at or below the marker)
		}

		body, err := ddlFS.ReadFile("ddl/" + name)
		if err != nil {
			return fmt.Errorf("read ddl script %s: %w", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin ddl script %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			tx.Rollback(context.Background()) //nolint:errcheck
			return fmt.Errorf("apply ddl script %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE last_synced SET synced_at = $1, updated_at = now() WHERE id = 1`, ts); err != nil {
			tx.Rollback(context.Background()) //nolint:errcheck
			return fmt.Errorf("advance last_synced for %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit ddl script %s: %w", name, err)
		}
		log.Info("ddl applied", "script", name, "synced_at", ts)
	}
	return nil
}

// ddlTimestamp extracts the DDMMYYYYHHMM prefix (12 digits) from a script
// filename, e.g. 180820260133_create_users.sql -> 180820260133.
func ddlTimestamp(name string) (int64, error) {
	base := strings.TrimSuffix(name, ".sql")
	if len(base) < 13 || base[12] != '_' { // 12 digits + underscore + description
		return 0, fmt.Errorf("expected <12 digit timestamp>_<name>.sql, got %q", name)
	}
	ts, err := strconv.ParseInt(base[:12], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("timestamp %q is not numeric", base[:12])
	}
	return ts, nil
}
