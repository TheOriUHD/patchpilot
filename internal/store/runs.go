package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/core"
)

// RunRepository persists update runs and host runs.
type RunRepository struct {
	db      *sql.DB
	logRoot string
}

// NewRunRepository creates a SQLite-backed run repository.
func NewRunRepository(db *DB, logRoot string) *RunRepository {
	return &RunRepository{db: db.SQLDB(), logRoot: logRoot}
}

// CreateRunWithHosts creates a run and pending host_run rows.
func (r *RunRepository) CreateRunWithHosts(ctx context.Context, input core.RunCreate, hostIDs []int64) (core.RunDetail, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return core.RunDetail{}, fmt.Errorf("begin create run: %w", err)
	}
	defer rollback(tx)

	startedAt := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
        INSERT INTO runs (trigger, schedule_id, status, started_at, summary)
        VALUES (?, ?, ?, ?, ?)
    `, input.Trigger, input.ScheduleID, input.Status, startedAt, input.Summary)
	if err != nil {
		return core.RunDetail{}, fmt.Errorf("insert run: %w", err)
	}
	runID, err := result.LastInsertId()
	if err != nil {
		return core.RunDetail{}, fmt.Errorf("run id: %w", err)
	}

	for _, hostID := range hostIDs {
		logPath := filepath.Join(r.logRoot, fmt.Sprintf("run-%d", runID), fmt.Sprintf("%d.log", hostID))
		_, err := tx.ExecContext(ctx, `
            INSERT INTO host_runs (run_id, host_id, status, log_path)
            VALUES (?, ?, 'pending', ?)
        `, runID, hostID, logPath)
		if err != nil {
			return core.RunDetail{}, fmt.Errorf("insert host run for host %d: %w", hostID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return core.RunDetail{}, fmt.Errorf("commit create run: %w", err)
	}
	return r.Get(ctx, runID)
}

// List returns recent runs in reverse chronological order.
func (r *RunRepository) List(ctx context.Context) ([]core.Run, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, trigger, schedule_id, status, started_at, finished_at, COALESCE(summary, '')
        FROM runs
        ORDER BY started_at DESC, id DESC
        LIMIT 200
    `)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	runs := make([]core.Run, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}
	return runs, nil
}

// Get returns a run with host runs.
func (r *RunRepository) Get(ctx context.Context, id int64) (core.RunDetail, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, trigger, schedule_id, status, started_at, finished_at, COALESCE(summary, '')
        FROM runs
        WHERE id = ?
    `, id)
	run, err := scanRun(row)
	if err != nil {
		return core.RunDetail{}, fmt.Errorf("get run %d: %w", id, err)
	}
	hostRuns, err := r.hostRuns(ctx, id)
	if err != nil {
		return core.RunDetail{}, err
	}
	return core.RunDetail{Run: run, HostRuns: hostRuns}, nil
}

// Active returns the currently running run, if any.
func (r *RunRepository) Active(ctx context.Context) (*core.Run, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, trigger, schedule_id, status, started_at, finished_at, COALESCE(summary, '')
        FROM runs
        WHERE status = 'running'
        ORDER BY started_at ASC, id ASC
        LIMIT 1
    `)
	run, err := scanRun(row)
	if err == nil {
		return &run, nil
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return nil, fmt.Errorf("get active run: %w", err)
}

// NextQueued returns the oldest queued manual run, if any.
func (r *RunRepository) NextQueued(ctx context.Context) (*core.RunDetail, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `SELECT id FROM runs WHERE status = 'queued' ORDER BY started_at ASC, id ASC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get next queued run: %w", err)
	}
	detail, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

// PromoteQueued marks a queued run running.
func (r *RunRepository) PromoteQueued(ctx context.Context, id int64) (core.RunDetail, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE runs SET status = 'running', started_at = CURRENT_TIMESTAMP, summary = 'promoted from manual queue' WHERE id = ? AND status = 'queued'`, id); err != nil {
		return core.RunDetail{}, fmt.Errorf("promote queued run %d: %w", id, err)
	}
	return r.Get(ctx, id)
}

// StartHostRun marks one host run running.
func (r *RunRepository) StartHostRun(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE host_runs SET status = 'running', started_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("start host run %d: %w", id, err)
	}
	return nil
}

// FinishHostRun records one host result.
func (r *RunRepository) FinishHostRun(ctx context.Context, id int64, status core.HostRunStatus, exitCode int, packages int, rebootRequired bool, summary string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE host_runs
        SET status = ?, exit_code = ?, finished_at = CURRENT_TIMESTAMP, packages_upgraded_count = ?, reboot_required = ?, error_summary = NULLIF(?, '')
        WHERE id = ?
    `, status, exitCode, packages, boolInt(rebootRequired), summary, id)
	if err != nil {
		return fmt.Errorf("finish host run %d: %w", id, err)
	}
	return nil
}

// FinishRun marks a run complete.
func (r *RunRepository) FinishRun(ctx context.Context, id int64, status core.RunStatus, summary string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE runs SET status = ?, finished_at = CURRENT_TIMESTAMP, summary = ? WHERE id = ?`, status, summary, id)
	if err != nil {
		return fmt.Errorf("finish run %d: %w", id, err)
	}
	return nil
}

// CancelRun marks a run cancelled with all unfinished host runs skipped.
func (r *RunRepository) CancelRun(ctx context.Context, id int64, summary string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cancel run: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `UPDATE host_runs SET status = 'skipped', finished_at = CURRENT_TIMESTAMP, error_summary = ? WHERE run_id = ? AND status IN ('pending', 'running')`, summary, id); err != nil {
		return fmt.Errorf("cancel host runs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET status = 'cancelled', finished_at = CURRENT_TIMESTAMP, summary = ? WHERE id = ?`, summary, id); err != nil {
		return fmt.Errorf("cancel run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cancel run: %w", err)
	}
	return nil
}

func (r *RunRepository) hostRuns(ctx context.Context, runID int64) ([]core.HostRun, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT hr.id, hr.run_id, hr.host_id, h.name, hr.status, hr.exit_code, hr.started_at, hr.finished_at,
               COALESCE(hr.log_path, ''), COALESCE(hr.packages_upgraded_count, 0), hr.reboot_required, COALESCE(hr.error_summary, '')
        FROM host_runs hr
        JOIN hosts h ON h.id = hr.host_id
        WHERE hr.run_id = ?
        ORDER BY hr.id ASC
    `, runID)
	if err != nil {
		return nil, fmt.Errorf("list host runs for run %d: %w", runID, err)
	}
	defer rows.Close()

	hostRuns := make([]core.HostRun, 0)
	for rows.Next() {
		hostRun, err := scanHostRun(rows)
		if err != nil {
			return nil, err
		}
		hostRuns = append(hostRuns, hostRun)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate host runs: %w", err)
	}
	return hostRuns, nil
}

func scanRun(scanner interface{ Scan(dest ...any) error }) (core.Run, error) {
	var run core.Run
	var trigger string
	var status string
	var scheduleID sql.NullInt64
	var finishedAt sql.NullTime
	if err := scanner.Scan(&run.ID, &trigger, &scheduleID, &status, &run.StartedAt, &finishedAt, &run.Summary); err != nil {
		return core.Run{}, err
	}
	run.Trigger = core.RunTrigger(trigger)
	run.Status = core.RunStatus(status)
	if scheduleID.Valid {
		run.ScheduleID = &scheduleID.Int64
	}
	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.Time
	}
	return run, nil
}

func scanHostRun(scanner interface{ Scan(dest ...any) error }) (core.HostRun, error) {
	var hostRun core.HostRun
	var status string
	var exitCode sql.NullInt64
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var reboot int
	if err := scanner.Scan(&hostRun.ID, &hostRun.RunID, &hostRun.HostID, &hostRun.HostName, &status, &exitCode, &startedAt, &finishedAt, &hostRun.LogPath, &hostRun.PackagesUpgradedCount, &reboot, &hostRun.ErrorSummary); err != nil {
		return core.HostRun{}, err
	}
	hostRun.Status = core.HostRunStatus(status)
	if exitCode.Valid {
		code := int(exitCode.Int64)
		hostRun.ExitCode = &code
	}
	if startedAt.Valid {
		hostRun.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		hostRun.FinishedAt = &finishedAt.Time
	}
	hostRun.RebootRequired = reboot == 1
	return hostRun, nil
}

func rollback(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}
