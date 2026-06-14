package store

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	return r.ListPage(ctx, core.RunListQuery{Limit: 200})
}

// ListPage returns filtered runs in reverse chronological order.
func (r *RunRepository) ListPage(ctx context.Context, query core.RunListQuery) ([]core.Run, error) {
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	where := []string{}
	args := []any{}
	if len(query.Statuses) > 0 {
		placeholders := make([]string, 0, len(query.Statuses))
		for _, status := range query.Statuses {
			placeholders = append(placeholders, "?")
			args = append(args, status)
		}
		where = append(where, "r.status IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(query.Triggers) > 0 {
		placeholders := make([]string, 0, len(query.Triggers))
		for _, trigger := range query.Triggers {
			placeholders = append(placeholders, "?")
			args = append(args, trigger)
		}
		where = append(where, "r.trigger IN ("+strings.Join(placeholders, ",")+")")
	}
	if query.Since != nil {
		where = append(where, "r.started_at >= ?")
		args = append(args, *query.Since)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id, r.trigger, r.schedule_id, COALESCE(s.name, ''), r.status, r.started_at, r.finished_at,
		       COUNT(hr.id), COALESCE(r.summary, '')
		FROM runs r
		LEFT JOIN schedules s ON s.id = r.schedule_id
		LEFT JOIN host_runs hr ON hr.run_id = r.id
		`+whereSQL+`
		GROUP BY r.id
		ORDER BY r.started_at DESC, r.id DESC
		LIMIT ? OFFSET ?
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	runs := make([]core.Run, 0)
	for rows.Next() {
		run, err := scanRunSummary(rows)
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

// Logs returns persisted log lines for a run.
func (r *RunRepository) Logs(ctx context.Context, id int64, maxLines int) (core.RunLogResponse, error) {
	if maxLines <= 0 || maxLines > 5000 {
		maxLines = 2000
	}
	hostRuns, err := r.hostRuns(ctx, id)
	if err != nil {
		return core.RunLogResponse{}, err
	}
	entries := make([]core.RunLogEntry, 0)
	for _, hostRun := range hostRuns {
		if err := ctx.Err(); err != nil {
			return core.RunLogResponse{}, err
		}
		file, err := os.Open(hostRun.LogPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return core.RunLogResponse{}, fmt.Errorf("open run log %s: %w", hostRun.LogPath, err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			entries = append(entries, core.RunLogEntry{HostRunID: hostRun.ID, HostID: hostRun.HostID, HostName: hostRun.HostName, Line: scanner.Text()})
			if len(entries) > maxLines {
				entries = entries[len(entries)-maxLines:]
			}
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return core.RunLogResponse{}, fmt.Errorf("read run log %s: %w", hostRun.LogPath, err)
		}
		if err := file.Close(); err != nil {
			return core.RunLogResponse{}, fmt.Errorf("close run log %s: %w", hostRun.LogPath, err)
		}
	}
	return core.RunLogResponse{Entries: entries}, nil
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

// HostInFlight reports whether a host is pending or running inside an active run.
func (r *RunRepository) HostInFlight(ctx context.Context, hostID int64) (bool, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `
				SELECT COUNT(1)
				FROM host_runs hr
				JOIN runs r ON r.id = hr.run_id
				WHERE hr.host_id = ?
					AND r.status IN ('queued', 'running')
					AND hr.status IN ('pending', 'running')
		`, hostID).Scan(&count); err != nil {
		return false, fmt.Errorf("check host %d in-flight run: %w", hostID, err)
	}
	return count > 0, nil
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
	defer func() { _ = rows.Close() }()

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

func scanRunSummary(scanner interface{ Scan(dest ...any) error }) (core.Run, error) {
	var run core.Run
	var trigger string
	var status string
	var scheduleID sql.NullInt64
	var finishedAt sql.NullTime
	if err := scanner.Scan(&run.ID, &trigger, &scheduleID, &run.ScheduleName, &status, &run.StartedAt, &finishedAt, &run.HostCount, &run.Summary); err != nil {
		return core.Run{}, err
	}
	run.Trigger = core.RunTrigger(trigger)
	run.Status = core.RunStatus(status)
	if scheduleID.Valid {
		run.ScheduleID = &scheduleID.Int64
	}
	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.Time
		run.DurationSeconds = int64(finishedAt.Time.Sub(run.StartedAt).Seconds())
	} else {
		run.DurationSeconds = int64(time.Since(run.StartedAt).Seconds())
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
