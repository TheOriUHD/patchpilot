package core

import "time"

// RunStatus describes the lifecycle state of an update run.
type RunStatus string

const (
	// RunStatusQueued is a durable manual run waiting behind an active run.
	RunStatusQueued RunStatus = "queued"
	// RunStatusRunning is a run currently being executed by the daemon.
	RunStatusRunning RunStatus = "running"
	// RunStatusSuccess is a run where every host completed successfully.
	RunStatusSuccess RunStatus = "success"
	// RunStatusPartialFailure is a run where at least one host failed.
	RunStatusPartialFailure RunStatus = "partial_failure"
	// RunStatusFailed is a run that failed before it could complete normally.
	RunStatusFailed RunStatus = "failed"
	// RunStatusCancelled is a run cancelled by an operator.
	RunStatusCancelled RunStatus = "cancelled"
)

// RunTrigger identifies why a run was created.
type RunTrigger string

const (
	// RunTriggerManual is a run started from the TUI.
	RunTriggerManual RunTrigger = "manual"
	// RunTriggerScheduled is a run started by the scheduler.
	RunTriggerScheduled RunTrigger = "scheduled"
	// RunTriggerCLI is a run started by the CLI.
	RunTriggerCLI RunTrigger = "cli"
)

// HostRunStatus describes one host's state inside a run.
type HostRunStatus string

const (
	// HostRunStatusPending means the host has not started yet.
	HostRunStatusPending HostRunStatus = "pending"
	// HostRunStatusRunning means the host is currently updating.
	HostRunStatusRunning HostRunStatus = "running"
	// HostRunStatusSuccess means the host completed successfully.
	HostRunStatusSuccess HostRunStatus = "success"
	// HostRunStatusFailed means the host failed.
	HostRunStatusFailed HostRunStatus = "failed"
	// HostRunStatusSkipped means the host was deliberately skipped.
	HostRunStatusSkipped HostRunStatus = "skipped"
)

// Run is the persisted top-level run record.
type Run struct {
	ID              int64      `json:"id"`
	Trigger         RunTrigger `json:"trigger"`
	ScheduleID      *int64     `json:"schedule_id,omitempty"`
	ScheduleName    string     `json:"schedule_name"`
	Status          RunStatus  `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	HostCount       int        `json:"host_count"`
	DurationSeconds int64      `json:"duration_seconds"`
	Summary         string     `json:"summary"`
}

// RunListQuery filters and paginates the global run log.
type RunListQuery struct {
	Limit    int
	Offset   int
	Statuses []RunStatus
	Triggers []RunTrigger
	Since    *time.Time
}

// HostRun is the persisted status of one host inside a run.
type HostRun struct {
	ID                    int64         `json:"id"`
	RunID                 int64         `json:"run_id"`
	HostID                int64         `json:"host_id"`
	HostName              string        `json:"host_name"`
	Status                HostRunStatus `json:"status"`
	ExitCode              *int          `json:"exit_code,omitempty"`
	StartedAt             *time.Time    `json:"started_at,omitempty"`
	FinishedAt            *time.Time    `json:"finished_at,omitempty"`
	LogPath               string        `json:"log_path"`
	PackagesUpgradedCount int           `json:"packages_upgraded_count"`
	RebootRequired        bool          `json:"reboot_required"`
	ErrorSummary          string        `json:"error_summary"`
}

// RunDetail combines a run with its per-host rows.
type RunDetail struct {
	Run      Run       `json:"run"`
	HostRuns []HostRun `json:"host_runs"`
}

// RunCreate describes a run to create.
type RunCreate struct {
	Trigger    RunTrigger `json:"trigger"`
	ScheduleID *int64     `json:"schedule_id,omitempty"`
	Status     RunStatus  `json:"status"`
	Summary    string     `json:"summary"`
}

// RunEvent is streamed to live run clients.
type RunEvent struct {
	RunID      int64         `json:"run_id"`
	HostRunID  int64         `json:"host_run_id,omitempty"`
	HostID     int64         `json:"host_id,omitempty"`
	HostName   string        `json:"host_name,omitempty"`
	RunStatus  RunStatus     `json:"run_status,omitempty"`
	HostStatus HostRunStatus `json:"host_status,omitempty"`
	Message    string        `json:"message"`
	Reboot     bool          `json:"reboot_required,omitempty"`
	OccurredAt time.Time     `json:"occurred_at"`
}

// RunLogResponse contains persisted log lines for historical run viewing.
type RunLogResponse struct {
	Entries []RunLogEntry `json:"entries"`
}

// RunLogEntry is one persisted host-run log line.
type RunLogEntry struct {
	HostRunID int64  `json:"host_run_id"`
	HostID    int64  `json:"host_id"`
	HostName  string `json:"host_name"`
	Line      string `json:"line"`
}
