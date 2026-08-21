package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
)

//go:embed ddl/*.sql
var ddlFS embed.FS

const initialLastSynced int64 = 180820260136

// SyncDDL applies timestamped SQL scripts newer than the stored marker.
func SyncDDL(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS last_synced (
			id         SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			synced_at  BIGINT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("ensure last_synced table: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO last_synced (id, synced_at) VALUES (1, ?) ON CONFLICT (id) DO NOTHING`,
		initialLastSynced); err != nil {
		return fmt.Errorf("seed last_synced: %w", err)
	}

	var syncedAt int64
	if err := db.QueryRowContext(ctx, `SELECT synced_at FROM last_synced WHERE id = 1`).Scan(&syncedAt); err != nil {
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
			continue
		}
		ts, err := ddlTimestamp(name)
		if err != nil {
			return fmt.Errorf("invalid ddl script name %q: %w", name, err)
		}
		if ts <= syncedAt {
			continue
		}
		body, err := ddlFS.ReadFile("ddl/" + name)
		if err != nil {
			return fmt.Errorf("read ddl script %s: %w", name, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin ddl script %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("apply ddl script %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE last_synced SET synced_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`, ts); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("advance last_synced for %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit ddl script %s: %w", name, err)
		}
		log.Info("ddl applied", "script", name, "synced_at", ts)
	}
	return nil
}

func ddlTimestamp(name string) (int64, error) {
	base := strings.TrimSuffix(name, ".sql")
	if len(base) < 13 || base[12] != '_' {
		return 0, fmt.Errorf("expected <12 digit timestamp>_<name>.sql, got %q", name)
	}
	ts, err := strconv.ParseInt(base[:12], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("timestamp %q is not numeric", base[:12])
	}
	return ts, nil
}
