package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/core"
)

// ScheduleRepository persists schedules and host assignments.
type ScheduleRepository struct {
	db *sql.DB
}

// NewScheduleRepository creates a SQLite-backed schedule repository.
func NewScheduleRepository(db *DB) *ScheduleRepository {
	return &ScheduleRepository{db: db.SQLDB()}
}

// List returns all schedules.
func (r *ScheduleRepository) List(ctx context.Context) ([]core.Schedule, error) {
	rows, err := r.db.QueryContext(ctx, scheduleSelect()+` GROUP BY s.id ORDER BY s.name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	return r.scanSchedules(ctx, rows)
}

// ListEnabled returns enabled schedules.
func (r *ScheduleRepository) ListEnabled(ctx context.Context) ([]core.Schedule, error) {
	rows, err := r.db.QueryContext(ctx, scheduleSelect()+` WHERE s.enabled = 1 GROUP BY s.id ORDER BY s.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list enabled schedules: %w", err)
	}
	return r.scanSchedules(ctx, rows)
}

// Get returns one schedule by id.
func (r *ScheduleRepository) Get(ctx context.Context, id int64) (core.Schedule, error) {
	row := r.db.QueryRowContext(ctx, scheduleSelect()+` WHERE s.id = ? GROUP BY s.id`, id)
	schedule, err := scanSchedule(row)
	if err != nil {
		return core.Schedule{}, fmt.Errorf("get schedule %d: %w", id, err)
	}
	hostIDs, err := r.hostIDs(ctx, id)
	if err != nil {
		return core.Schedule{}, err
	}
	schedule.HostIDs = hostIDs
	return schedule, nil
}

// Create inserts a schedule.
func (r *ScheduleRepository) Create(ctx context.Context, input core.ScheduleInput, nextRunAt *time.Time) (core.Schedule, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Schedule{}, fmt.Errorf("begin create schedule: %w", err)
	}
	defer rollback(tx)
	result, err := tx.ExecContext(ctx, `
        INSERT INTO schedules (name, frequency, minute, hour, day_of_week, day_of_month, enabled, next_run_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, input.Name, input.Frequency, input.Minute, input.Hour, input.DayOfWeek, input.DayOfMonth, boolInt(input.Enabled), nextRunAt)
	if err != nil {
		return core.Schedule{}, fmt.Errorf("insert schedule: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return core.Schedule{}, fmt.Errorf("schedule id: %w", err)
	}
	if err := replaceScheduleHosts(ctx, tx, id, input.HostIDs); err != nil {
		return core.Schedule{}, err
	}
	if err := tx.Commit(); err != nil {
		return core.Schedule{}, fmt.Errorf("commit create schedule: %w", err)
	}
	return r.Get(ctx, id)
}

// Update mutates a schedule and host assignments.
func (r *ScheduleRepository) Update(ctx context.Context, id int64, input core.ScheduleInput, nextRunAt *time.Time) (core.Schedule, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Schedule{}, fmt.Errorf("begin update schedule: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `
        UPDATE schedules
        SET name = ?, frequency = ?, minute = ?, hour = ?, day_of_week = ?, day_of_month = ?, enabled = ?, next_run_at = ?
        WHERE id = ?
    `, input.Name, input.Frequency, input.Minute, input.Hour, input.DayOfWeek, input.DayOfMonth, boolInt(input.Enabled), nextRunAt, id); err != nil {
		return core.Schedule{}, fmt.Errorf("update schedule %d: %w", id, err)
	}
	if err := replaceScheduleHosts(ctx, tx, id, input.HostIDs); err != nil {
		return core.Schedule{}, err
	}
	if err := tx.Commit(); err != nil {
		return core.Schedule{}, fmt.Errorf("commit update schedule: %w", err)
	}
	return r.Get(ctx, id)
}

// Delete removes a schedule.
func (r *ScheduleRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete schedule %d: %w", id, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted schedule %d: %w", id, err)
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkFired records a schedule firing and next time.
func (r *ScheduleRepository) MarkFired(ctx context.Context, id int64, firedAt time.Time, nextRunAt *time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE schedules SET last_run_at = ?, next_run_at = ?, last_skipped_at = NULL, last_skip_reason = '' WHERE id = ?`, firedAt, nextRunAt, id)
	if err != nil {
		return fmt.Errorf("mark schedule %d fired: %w", id, err)
	}
	return nil
}

// MarkSkipped records a skipped scheduled fire and next time.
func (r *ScheduleRepository) MarkSkipped(ctx context.Context, id int64, skippedAt time.Time, reason string, nextRunAt *time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE schedules SET last_skipped_at = ?, last_skip_reason = ?, next_run_at = ? WHERE id = ?`, skippedAt, reason, nextRunAt, id)
	if err != nil {
		return fmt.Errorf("mark schedule %d skipped: %w", id, err)
	}
	return nil
}

func (r *ScheduleRepository) scanSchedules(ctx context.Context, rows *sql.Rows) ([]core.Schedule, error) {
	schedules := make([]core.Schedule, 0)
	for rows.Next() {
		schedule, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedules: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close schedules rows: %w", err)
	}
	for index := range schedules {
		hostIDs, err := r.hostIDs(ctx, schedules[index].ID)
		if err != nil {
			return nil, err
		}
		schedules[index].HostIDs = hostIDs
	}
	return schedules, nil
}

func (r *ScheduleRepository) hostIDs(ctx context.Context, scheduleID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT host_id FROM schedule_hosts WHERE schedule_id = ? ORDER BY order_index ASC, host_id ASC`, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("list schedule hosts: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedule hosts: %w", err)
	}
	return ids, nil
}

func replaceScheduleHosts(ctx context.Context, tx *sql.Tx, scheduleID int64, hostIDs []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM schedule_hosts WHERE schedule_id = ?`, scheduleID); err != nil {
		return fmt.Errorf("clear schedule hosts: %w", err)
	}
	for index, hostID := range hostIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_hosts (schedule_id, host_id, order_index) VALUES (?, ?, ?)`, scheduleID, hostID, index); err != nil {
			return fmt.Errorf("insert schedule host %d: %w", hostID, err)
		}
	}
	return nil
}

func scheduleSelect() string {
	return `
        SELECT s.id, s.name, s.frequency, COALESCE(s.minute, 0), COALESCE(s.hour, 0), COALESCE(s.day_of_week, 0), COALESCE(s.day_of_month, 1),
               s.enabled, s.last_run_at, s.next_run_at, s.last_skipped_at, COALESCE(s.last_skip_reason, ''), s.created_at, s.updated_at,
               COUNT(sh.host_id) AS host_count
        FROM schedules s
        LEFT JOIN schedule_hosts sh ON sh.schedule_id = s.id
    `
}

func scanSchedule(scanner interface{ Scan(dest ...any) error }) (core.Schedule, error) {
	var schedule core.Schedule
	var frequency string
	var enabled int
	var lastRun sql.NullTime
	var nextRun sql.NullTime
	var lastSkipped sql.NullTime
	if err := scanner.Scan(&schedule.ID, &schedule.Name, &frequency, &schedule.Minute, &schedule.Hour, &schedule.DayOfWeek, &schedule.DayOfMonth, &enabled, &lastRun, &nextRun, &lastSkipped, &schedule.LastSkipReason, &schedule.CreatedAt, &schedule.UpdatedAt, &schedule.HostCount); err != nil {
		return core.Schedule{}, err
	}
	schedule.Frequency = core.Frequency(frequency)
	schedule.Enabled = enabled == 1
	if lastRun.Valid {
		schedule.LastRunAt = &lastRun.Time
	}
	if nextRun.Valid {
		schedule.NextRunAt = &nextRun.Time
	}
	if lastSkipped.Valid {
		schedule.LastSkippedAt = &lastSkipped.Time
	}
	return schedule, nil
}
