package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/core"
)

// RunRepository persists manual host runs for Milestone 2.
type RunRepository struct {
	db *sql.DB
}

// NewRunRepository creates a SQLite-backed run repository.
func NewRunRepository(db *DB) *RunRepository {
	return &RunRepository{db: db.SQLDB()}
}

// CreateManualHostRun creates a run and host_run pair for a manual single-host update.
func (r *RunRepository) CreateManualHostRun(ctx context.Context, hostID int64, logPath string) (int64, int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin manual run: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, `INSERT INTO runs (trigger, status, started_at, summary) VALUES ('manual', ?, ?, '')`, core.RunStatusRunning, time.Now().UTC())
	if err != nil {
		return 0, 0, fmt.Errorf("insert manual run: %w", err)
	}
	runID, err := result.LastInsertId()
	if err != nil {
		return 0, 0, fmt.Errorf("manual run id: %w", err)
	}

	result, err = tx.ExecContext(ctx, `
        INSERT INTO host_runs (run_id, host_id, status, started_at, log_path)
        VALUES (?, ?, 'running', ?, ?)
    `, runID, hostID, time.Now().UTC(), logPath)
	if err != nil {
		return 0, 0, fmt.Errorf("insert manual host run: %w", err)
	}
	hostRunID, err := result.LastInsertId()
	if err != nil {
		return 0, 0, fmt.Errorf("manual host run id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit manual run: %w", err)
	}
	tx = nil
	return runID, hostRunID, nil
}

// FinishHostRun records the outcome of a manual host run.
func (r *RunRepository) FinishHostRun(ctx context.Context, runID int64, hostRunID int64, status string, exitCode int, packages int, rebootRequired bool, summary string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finish host run: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `
        UPDATE host_runs
        SET status = ?, exit_code = ?, finished_at = CURRENT_TIMESTAMP, packages_upgraded_count = ?, reboot_required = ?, error_summary = NULLIF(?, '')
        WHERE id = ?
    `, status, exitCode, packages, boolInt(rebootRequired), summary, hostRunID)
	if err != nil {
		return fmt.Errorf("finish host run: %w", err)
	}

	runStatus := core.RunStatusSuccess
	if status != "success" {
		runStatus = core.RunStatusFailed
	}
	_, err = tx.ExecContext(ctx, `UPDATE runs SET status = ?, finished_at = CURRENT_TIMESTAMP, summary = ? WHERE id = ?`, runStatus, summary, runID)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit finish run: %w", err)
	}
	tx = nil
	return nil
}
