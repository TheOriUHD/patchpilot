package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection used by patchpilot repositories.
type DB struct {
	conn *sql.DB
}

// Open creates a SQLite connection and applies runtime pragmas.
func Open(ctx context.Context, databasePath string) (*DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	conn, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	conn.SetMaxOpenConns(1)

	db := &DB{conn: conn}
	if err := db.configure(ctx); err != nil {
		closeErr := conn.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("configure sqlite database: %w; close database: %w", err, closeErr)
		}
		return nil, err
	}
	return db, nil
}

// Close releases the SQLite connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// SQLDB returns the underlying database handle for composed repositories.
func (db *DB) SQLDB() *sql.DB {
	return db.conn
}

// RecoverOrphanedRuns marks interrupted running runs as failed while preserving queued runs.
func (db *DB) RecoverOrphanedRuns(ctx context.Context, reason string) (int64, error) {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin run recovery transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `
        UPDATE host_runs
        SET status = 'failed', finished_at = CURRENT_TIMESTAMP, error_summary = ?
        WHERE status IN ('pending', 'running')
          AND run_id IN (SELECT id FROM runs WHERE status = 'running')
    `, reason)
	if err != nil {
		return 0, fmt.Errorf("recover orphaned host runs: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
        UPDATE runs
        SET status = 'failed', finished_at = CURRENT_TIMESTAMP, summary = ?
        WHERE status = 'running'
    `, reason)
	if err != nil {
		return 0, fmt.Errorf("recover orphaned runs: %w", err)
	}

	recovered, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count recovered runs: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit run recovery: %w", err)
	}
	tx = nil
	return recovered, nil
}

func (db *DB) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, pragma := range pragmas {
		if _, err := db.conn.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	if err := db.conn.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite database: %w", err)
	}
	return nil
}
