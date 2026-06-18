package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/haos"
	"github.com/TheOriUHD/patchpilot/internal/pkgmgr"
	"github.com/TheOriUHD/patchpilot/internal/selfupdate"
	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
	"github.com/TheOriUHD/patchpilot/internal/stream"
	"github.com/TheOriUHD/patchpilot/internal/truenas"
	golangssh "golang.org/x/crypto/ssh"
)

// ErrScheduledOverlap is returned when a scheduled fire is skipped because another run is active.
var ErrScheduledOverlap = errors.New("scheduled run skipped because another run is active")

// OrchestratorRunStore is the run persistence interface required by the orchestrator.
type OrchestratorRunStore interface {
	CreateRunWithHosts(ctx context.Context, input RunCreate, hostIDs []int64) (RunDetail, error)
	List(ctx context.Context) ([]Run, error)
	Get(ctx context.Context, id int64) (RunDetail, error)
	Active(ctx context.Context) (*Run, error)
	NextQueued(ctx context.Context) (*RunDetail, error)
	PromoteQueued(ctx context.Context, id int64) (RunDetail, error)
	StartHostRun(ctx context.Context, id int64) error
	FinishHostRun(ctx context.Context, id int64, status HostRunStatus, exitCode int, packages int, rebootRequired bool, summary string) error
	FinishRun(ctx context.Context, id int64, status RunStatus, summary string) error
	CancelRun(ctx context.Context, id int64, summary string) error
}

// OrchestratorRunPageStore optionally provides paginated run listings.
type OrchestratorRunPageStore interface {
	ListPage(ctx context.Context, query RunListQuery) ([]Run, error)
}

// OrchestratorRunLogStore optionally provides historical run logs.
type OrchestratorRunLogStore interface {
	Logs(ctx context.Context, id int64, maxLines int) (RunLogResponse, error)
}

// OrchestratorHostRunStore optionally checks whether a host has an in-flight host run.
type OrchestratorHostRunStore interface {
	HostInFlight(ctx context.Context, hostID int64) (bool, error)
}

// OrchestratorScheduleStore is the schedule persistence interface required by the orchestrator.
type OrchestratorScheduleStore interface {
	List(ctx context.Context) ([]Schedule, error)
	ListEnabled(ctx context.Context) ([]Schedule, error)
	Get(ctx context.Context, id int64) (Schedule, error)
	Create(ctx context.Context, input ScheduleInput, nextRunAt *time.Time) (Schedule, error)
	Update(ctx context.Context, id int64, input ScheduleInput, nextRunAt *time.Time) (Schedule, error)
	Delete(ctx context.Context, id int64) error
	MarkFired(ctx context.Context, id int64, firedAt time.Time, nextRunAt *time.Time) error
	MarkSkipped(ctx context.Context, id int64, skippedAt time.Time, reason string, nextRunAt *time.Time) error
}

// SettingsStore provides runtime settings used by the orchestrator.
type SettingsStore interface {
	ConcurrencyEnabled(ctx context.Context) (bool, error)
}

// SelfUpdateScheduler schedules delayed self-updates.
type SelfUpdateScheduler interface {
	Schedule(ctx context.Context, runID int64) (string, string, error)
}

type standardHostRunner interface {
	remotessh.Runner
	RunStream(ctx context.Context, command string, stdin []byte, onChunk func(remotessh.StreamChunk)) (remotessh.CommandResult, error)
}

type standardHostConnector func(ctx context.Context, host string, port int, user string, signer golangssh.Signer) (standardHostRunner, error)

// Orchestrator coordinates schedule/manual/CLI update runs.
type Orchestrator struct {
	Hosts       HostRepository
	Runs        OrchestratorRunStore
	Schedules   OrchestratorScheduleStore
	Audit       AuditRepository
	Keys        *remotessh.KeyStore
	Secrets     SecretCipher
	Settings    SettingsStore
	SelfUpdater SelfUpdateScheduler
	Streams     *stream.Hub
	Logger      *slog.Logger
	ConnectKey  standardHostConnector

	mu           sync.Mutex
	activeRunID  int64
	activeCancel context.CancelFunc
}

// SetSettingsStore attaches runtime settings after construction.
func (o *Orchestrator) SetSettingsStore(settings SettingsStore) {
	o.Settings = settings
}

// SetSelfUpdater attaches the self-update scheduler after construction.
func (o *Orchestrator) SetSelfUpdater(updater SelfUpdateScheduler) {
	o.SelfUpdater = updater
}

// NewOrchestrator creates a run orchestrator.
func NewOrchestrator(hosts HostRepository, runs OrchestratorRunStore, schedules OrchestratorScheduleStore, audit AuditRepository, keys *remotessh.KeyStore, streams *stream.Hub, logger *slog.Logger, secrets ...SecretCipher) *Orchestrator {
	var cipher SecretCipher
	if len(secrets) > 0 {
		cipher = secrets[0]
	}
	return &Orchestrator{Hosts: hosts, Runs: runs, Schedules: schedules, Audit: audit, Keys: keys, Secrets: cipher, Streams: streams, Logger: logger}
}

// Start resumes the oldest queued manual/CLI run if no run is active after crash recovery.
func (o *Orchestrator) Start(ctx context.Context) {
	go o.promoteNextQueued(ctx)
}

// RunStreamID returns the event stream id for a run.
func RunStreamID(runID int64) string {
	return fmt.Sprintf("run-%d", runID)
}

// ListRuns returns recent runs.

func (o *Orchestrator) ListRuns(ctx context.Context) ([]Run, error) {
	return o.ListRunsPage(ctx, RunListQuery{Limit: 100})
}

// ListRunsPage returns paginated global run history.
func (o *Orchestrator) ListRunsPage(ctx context.Context, query RunListQuery) ([]Run, error) {
	if store, ok := o.Runs.(OrchestratorRunPageStore); ok {
		return store.ListPage(ctx, query)
	}
	return o.Runs.List(ctx)
}

// RunLogs returns persisted logs for a historical run.
func (o *Orchestrator) RunLogs(ctx context.Context, id int64, maxLines int) (RunLogResponse, error) {
	if store, ok := o.Runs.(OrchestratorRunLogStore); ok {
		return store.Logs(ctx, id, maxLines)
	}
	return RunLogResponse{}, fmt.Errorf("run logs are not available")
}

// RecordAudit writes an audit event through the orchestrator's audit sink.
func (o *Orchestrator) RecordAudit(ctx context.Context, actor string, action string, targetType string, targetID *int64, details map[string]any) {
	o.audit(ctx, actor, action, targetType, targetID, details)
}

// GetRun returns one run with host runs.
func (o *Orchestrator) GetRun(ctx context.Context, id int64) (RunDetail, error) {
	return o.Runs.Get(ctx, id)
}

// HostInFlight reports whether a host currently has a pending or running host run.
func (o *Orchestrator) HostInFlight(ctx context.Context, hostID int64) (bool, error) {
	if store, ok := o.Runs.(OrchestratorHostRunStore); ok {
		return store.HostInFlight(ctx, hostID)
	}
	active, err := o.Runs.Active(ctx)
	if err != nil || active == nil {
		return false, err
	}
	detail, err := o.Runs.Get(ctx, active.ID)
	if err != nil {
		return false, err
	}
	for _, hostRun := range detail.HostRuns {
		if hostRun.HostID == hostID && (hostRun.Status == HostRunStatusPending || hostRun.Status == HostRunStatusRunning) {
			return true, nil
		}
	}
	return false, nil
}

// RunHostNow creates or queues a single-host run.
func (o *Orchestrator) RunHostNow(ctx context.Context, hostID int64, trigger RunTrigger, actor string) (RunDetail, error) {
	host, err := o.Hosts.Get(ctx, hostID)
	if err != nil {
		return RunDetail{}, err
	}
	if !host.Enabled {
		return RunDetail{}, fmt.Errorf("host %s is disabled", host.Name)
	}
	return o.createOrQueue(ctx, []Host{host}, nil, trigger, actor, "single host run", nil)
}

// RunSchedule creates, queues, or skips a schedule run.
func (o *Orchestrator) RunSchedule(ctx context.Context, scheduleID int64, trigger RunTrigger, actor string, nextRunAt *time.Time) (RunDetail, error) {
	schedule, err := o.Schedules.Get(ctx, scheduleID)
	if err != nil {
		return RunDetail{}, err
	}
	hosts := make([]Host, 0, len(schedule.HostIDs))
	for _, hostID := range schedule.HostIDs {
		host, err := o.Hosts.Get(ctx, hostID)
		if err != nil {
			return RunDetail{}, err
		}
		if host.Enabled {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 {
		return RunDetail{}, fmt.Errorf("schedule %s has no enabled hosts", schedule.Name)
	}
	return o.createOrQueue(ctx, hosts, &schedule.ID, trigger, actor, "schedule "+schedule.Name, nextRunAt)
}

// CancelRun cancels the currently active run or drops a queued run.
func (o *Orchestrator) CancelRun(ctx context.Context, runID int64, actor string) error {
	o.mu.Lock()
	if o.activeRunID == runID && o.activeCancel != nil {
		o.activeCancel()
	}
	o.mu.Unlock()
	if err := o.Runs.CancelRun(ctx, runID, "cancelled by "+actor); err != nil {
		return err
	}
	o.audit(ctx, actor, "run.cancel", "run", &runID, map[string]any{"run_id": runID})
	o.publish(runID, RunEvent{RunID: runID, RunStatus: RunStatusCancelled, Message: "run cancelled", OccurredAt: time.Now().UTC()})
	return nil
}

func (o *Orchestrator) createOrQueue(ctx context.Context, hosts []Host, scheduleID *int64, trigger RunTrigger, actor string, summary string, nextRunAt *time.Time) (RunDetail, error) {
	hostIDs := make([]int64, 0, len(hosts))
	for _, host := range orderHosts(hosts) {
		hostIDs = append(hostIDs, host.ID)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	active, err := o.hasActiveLocked(ctx)
	if err != nil {
		return RunDetail{}, err
	}
	if active {
		if trigger == RunTriggerScheduled {
			reason := "another run is active"
			if scheduleID != nil {
				_ = o.Schedules.MarkSkipped(ctx, *scheduleID, time.Now().UTC(), reason, nextRunAt)
			}
			o.audit(ctx, "scheduler", "run.skipped_overlap", "schedule", scheduleID, map[string]any{"reason": reason})
			return RunDetail{}, ErrScheduledOverlap
		}
		detail, err := o.Runs.CreateRunWithHosts(ctx, RunCreate{Trigger: trigger, ScheduleID: scheduleID, Status: RunStatusQueued, Summary: summary + " queued behind active run"}, hostIDs)
		if err != nil {
			return RunDetail{}, err
		}
		if o.Streams != nil {
			o.Streams.Ensure(RunStreamID(detail.Run.ID))
		}
		o.audit(ctx, actor, "run.queued", "run", &detail.Run.ID, map[string]any{"trigger": trigger, "schedule_id": scheduleID})
		return detail, nil
	}

	detail, err := o.Runs.CreateRunWithHosts(ctx, RunCreate{Trigger: trigger, ScheduleID: scheduleID, Status: RunStatusRunning, Summary: summary}, hostIDs)
	if err != nil {
		return RunDetail{}, err
	}
	if trigger == RunTriggerScheduled && scheduleID != nil {
		_ = o.Schedules.MarkFired(ctx, *scheduleID, time.Now().UTC(), nextRunAt)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	o.activeRunID = detail.Run.ID
	o.activeCancel = cancel
	if o.Streams != nil {
		o.Streams.Ensure(RunStreamID(detail.Run.ID))
	}
	o.audit(ctx, actor, "run.start", "run", &detail.Run.ID, map[string]any{"trigger": trigger, "schedule_id": scheduleID})
	go o.executeRun(runCtx, detail)
	return detail, nil
}

func (o *Orchestrator) hasActiveLocked(ctx context.Context) (bool, error) {
	if o.activeRunID != 0 {
		return true, nil
	}
	active, err := o.Runs.Active(ctx)
	if err != nil {
		return false, err
	}
	return active != nil, nil
}

func (o *Orchestrator) promoteNextQueued(ctx context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()
	active, err := o.hasActiveLocked(ctx)
	if err != nil || active {
		if err != nil && o.Logger != nil {
			o.Logger.Error("check active run for queue promotion", "error", err)
		}
		return
	}
	detail, err := o.Runs.NextQueued(ctx)
	if err != nil {
		if o.Logger != nil {
			o.Logger.Error("load next queued run", "error", err)
		}
		return
	}
	if detail == nil {
		return
	}
	promoted, err := o.Runs.PromoteQueued(ctx, detail.Run.ID)
	if err != nil {
		if o.Logger != nil {
			o.Logger.Error("promote queued run", "error", err, "run_id", detail.Run.ID)
		}
		return
	}
	runCtx, cancel := context.WithCancel(context.Background())
	o.activeRunID = promoted.Run.ID
	o.activeCancel = cancel
	if o.Streams != nil {
		o.Streams.Ensure(RunStreamID(promoted.Run.ID))
	}
	o.audit(ctx, "daemon", "run.promote", "run", &promoted.Run.ID, map[string]any{"trigger": promoted.Run.Trigger})
	go o.executeRun(runCtx, promoted)
}

func (o *Orchestrator) executeRun(ctx context.Context, detail RunDetail) {
	runID := detail.Run.ID
	if o.Streams != nil {
		defer o.Streams.Complete(RunStreamID(runID))
	}
	o.publish(runID, RunEvent{RunID: runID, RunStatus: RunStatusRunning, Message: "run started", OccurredAt: time.Now().UTC()})

	successCount, failureCount, skippedCount, cancelled := o.executeRunTasks(ctx, detail)
	if cancelled {
		_ = o.Runs.CancelRun(context.Background(), runID, "cancelled")
		o.finishActive(runID)
		o.publish(runID, RunEvent{RunID: runID, RunStatus: RunStatusCancelled, Message: "run cancelled", OccurredAt: time.Now().UTC()})
		return
	}

	runStatus := RunStatusSuccess
	summary := fmt.Sprintf("%d succeeded, %d failed, %d skipped", successCount, failureCount, skippedCount)
	if failureCount > 0 && successCount == 0 {
		runStatus = RunStatusFailed
	} else if failureCount > 0 || skippedCount > 0 {
		runStatus = RunStatusPartialFailure
	}
	if err := o.Runs.FinishRun(context.Background(), runID, runStatus, summary); err != nil && o.Logger != nil {
		o.Logger.Error("finish run", "error", err, "run_id", runID)
	}
	o.audit(context.Background(), "daemon", "run.finish", "run", &runID, map[string]any{"status": runStatus, "summary": summary})
	o.publish(runID, RunEvent{RunID: runID, RunStatus: runStatus, Message: summary, OccurredAt: time.Now().UTC()})
	o.finishActive(runID)
}

func (o *Orchestrator) finishActive(runID int64) {
	o.mu.Lock()
	if o.activeRunID == runID {
		o.activeRunID = 0
		o.activeCancel = nil
	}
	o.mu.Unlock()
	o.promoteNextQueued(context.Background())
}

type runTask struct {
	Host           Host
	HostRun        HostRun
	SuppressReboot bool
}

func (o *Orchestrator) executeRunTasks(ctx context.Context, detail RunDetail) (int, int, int, bool) {
	tasks := make([]runTask, 0, len(detail.HostRuns))
	successCount := 0
	failureCount := 0
	skippedCount := 0
	suppressReboot := false
	if detail.Run.ScheduleID != nil {
		schedule, err := o.Schedules.Get(ctx, *detail.Run.ScheduleID)
		if err != nil {
			failureCount += len(detail.HostRuns)
			for _, hostRun := range detail.HostRuns {
				_ = o.Runs.FinishHostRun(context.Background(), hostRun.ID, HostRunStatusFailed, -1, 0, false, err.Error())
			}
			return 0, failureCount, 0, false
		}
		suppressReboot = schedule.SuppressReboot
	}
	for _, hostRun := range detail.HostRuns {
		host, err := o.Hosts.Get(ctx, hostRun.HostID)
		if err != nil {
			failureCount++
			_ = o.Runs.FinishHostRun(context.Background(), hostRun.ID, HostRunStatusFailed, -1, 0, false, err.Error())
			continue
		}
		tasks = append(tasks, runTask{Host: host, HostRun: hostRun, SuppressReboot: suppressReboot})
	}
	maxConcurrent := o.maxConcurrentForRun(ctx, detail)
	for group := 0; group < 5; group++ {
		groupTasks := make([]runTask, 0)
		for _, task := range tasks {
			if hostExecutionGroup(task.Host) == group {
				groupTasks = append(groupTasks, task)
			}
		}
		if len(groupTasks) == 0 {
			continue
		}
		var outcomes []hostOutcome
		if group == 0 && maxConcurrent > 1 {
			outcomes = o.executeTaskGroupParallel(ctx, detail.Run.ID, groupTasks, maxConcurrent)
		} else {
			outcomes = o.executeTaskGroupSequential(ctx, detail.Run.ID, groupTasks)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return successCount, failureCount, skippedCount, true
		}
		for _, outcome := range outcomes {
			switch outcome.Status {
			case HostRunStatusSuccess:
				successCount++
			case HostRunStatusSkipped:
				skippedCount++
			default:
				failureCount++
			}
		}
	}
	return successCount, failureCount, skippedCount, false
}

func (o *Orchestrator) executeTaskGroupSequential(ctx context.Context, runID int64, tasks []runTask) []hostOutcome {
	outcomes := make([]hostOutcome, 0, len(tasks))
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return outcomes
		}
		outcomes = append(outcomes, o.executeRunTask(ctx, runID, task))
	}
	return outcomes
}

func (o *Orchestrator) executeTaskGroupParallel(ctx context.Context, runID int64, tasks []runTask, maxConcurrent int) []hostOutcome {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	sem := make(chan struct{}, maxConcurrent)
	results := make(chan hostOutcome, len(tasks))
	var waitGroup sync.WaitGroup
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			break
		}
		sem <- struct{}{}
		waitGroup.Add(1)
		go func(task runTask) {
			defer waitGroup.Done()
			defer func() { <-sem }()
			results <- o.executeRunTask(ctx, runID, task)
		}(task)
	}
	waitGroup.Wait()
	close(results)
	outcomes := make([]hostOutcome, 0, len(tasks))
	for outcome := range results {
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func (o *Orchestrator) executeRunTask(ctx context.Context, runID int64, task runTask) hostOutcome {
	host := task.Host
	hostRun := task.HostRun
	if err := o.Runs.StartHostRun(context.Background(), hostRun.ID); err != nil && o.Logger != nil {
		o.Logger.Error("start host run", "error", err, "host_run_id", hostRun.ID)
	}
	o.publish(runID, RunEvent{RunID: runID, HostRunID: hostRun.ID, HostID: host.ID, HostName: host.Name, HostStatus: HostRunStatusRunning, Message: "host update started", OccurredAt: time.Now().UTC()})
	outcome := o.executeHostRun(ctx, host, hostRun, task.SuppressReboot)
	if task.SuppressReboot && outcome.RebootRequired {
		appendCleanLog(hostRun.LogPath, "reboot suppressed by schedule policy\n")
		outcome.RebootRequired = false
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return outcome
	}
	if err := o.Runs.FinishHostRun(context.Background(), hostRun.ID, outcome.Status, outcome.ExitCode, outcome.Packages, outcome.RebootRequired, outcome.Summary); err != nil && o.Logger != nil {
		o.Logger.Error("finish host run", "error", err, "host_run_id", hostRun.ID)
	}
	o.publish(runID, RunEvent{RunID: runID, HostRunID: hostRun.ID, HostID: host.ID, HostName: host.Name, HostStatus: outcome.Status, Message: outcome.Summary, Reboot: outcome.RebootRequired, OccurredAt: time.Now().UTC()})
	return outcome
}

func (o *Orchestrator) maxConcurrentForRun(ctx context.Context, detail RunDetail) int {
	if detail.Run.ScheduleID == nil || o.Settings == nil {
		return 1
	}
	enabled, err := o.Settings.ConcurrencyEnabled(ctx)
	if err != nil || !enabled {
		if err != nil && o.Logger != nil {
			o.Logger.Error("read concurrency setting", "error", err)
		}
		return 1
	}
	schedule, err := o.Schedules.Get(ctx, *detail.Run.ScheduleID)
	if err != nil {
		if o.Logger != nil {
			o.Logger.Error("read schedule concurrency", "error", err, "schedule_id", *detail.Run.ScheduleID)
		}
		return 1
	}
	if schedule.MaxConcurrent < 2 {
		return 1
	}
	if schedule.MaxConcurrent > 10 {
		return 10
	}
	return schedule.MaxConcurrent
}

func hostExecutionGroup(host Host) int {
	switch {
	case host.Type == HostTypeTrueNAS || pkgmgr.PackageManager(host.PackageManager) == pkgmgr.ManagerTrueNASAPI:
		return 1
	case host.Type == HostTypeHAOS || pkgmgr.PackageManager(host.PackageManager) == pkgmgr.ManagerHAOSAPI:
		return 2
	case host.IsProxmoxHost:
		return 3
	case host.IsSelf:
		return 4
	default:
		return 0
	}
}

func (o *Orchestrator) executeHostRun(ctx context.Context, host Host, hostRun HostRun, suppressReboot bool) hostOutcome {
	if host.Type == HostTypeTrueNAS || pkgmgr.PackageManager(host.PackageManager) == pkgmgr.ManagerTrueNASAPI {
		return o.executeTrueNASRun(ctx, host, hostRun)
	}
	if host.Type == HostTypeHAOS || pkgmgr.PackageManager(host.PackageManager) == pkgmgr.ManagerHAOSAPI {
		return o.executeHAOSRun(ctx, host, hostRun)
	}
	if host.IsSelf {
		return o.executeSelfUpdateRun(ctx, host, hostRun)
	}

	if err := os.MkdirAll(filepath.Dir(hostRun.LogPath), 0o755); err != nil {
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	file, err := os.OpenFile(hostRun.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	defer func() { _ = file.Close() }()
	_, _ = fmt.Fprintf(file, "patchpilot host %s started at %s\n", host.Name, time.Now().UTC().Format(time.RFC3339))

	keypair, err := o.Keys.Ensure(ctx)
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	connector := o.ConnectKey
	if connector == nil {
		connector = func(ctx context.Context, host string, port int, user string, signer golangssh.Signer) (standardHostRunner, error) {
			return remotessh.ConnectKey(ctx, host, port, user, signer)
		}
	}
	runner, err := connector(ctx, host.Hostname, normalizePort(host.SSHPort), host.SSHUser, keypair.Signer)
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	defer func() { _ = runner.Close() }()

	manager := pkgmgr.PackageManager(host.PackageManager)
	paths, err := resolveCommandPaths(ctx, runner, nil, HostFacts{CurrentUID: "0", PackageManager: manager}, manager, nil)
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	command, err := pkgmgr.UpdateCommand(manager, paths)
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	cleaner := ansiStripper{}
	var writeMu sync.Mutex
	result, err := runner.RunStream(ctx, command, nil, func(chunk remotessh.StreamChunk) {
		writeMu.Lock()
		defer writeMu.Unlock()
		clean := cleaner.Clean(string(chunk.Data))
		if clean == "" {
			return
		}
		_, _ = file.WriteString(clean)
	})
	if err != nil {
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: result.ExitCode, Summary: err.Error()}
	}
	rebootRequired := false
	if suppressReboot {
		appendCleanLog(hostRun.LogPath, "reboot detection skipped: suppressed by schedule policy\n")
	} else {
		rebootRequired = o.checkRebootRequired(ctx, runner, manager, paths, hostRun.LogPath)
	}
	return hostOutcome{Status: HostRunStatusSuccess, ExitCode: result.ExitCode, RebootRequired: rebootRequired, Summary: "update completed"}
}

func (o *Orchestrator) executeSelfUpdateRun(ctx context.Context, host Host, hostRun HostRun) hostOutcome {
	appendCleanLog(hostRun.LogPath, fmt.Sprintf("patchpilot self host %s reached at %s\n", host.Name, time.Now().UTC().Format(time.RFC3339)))
	if o.SelfUpdater == nil {
		message := "self-update scheduler is not configured"
		appendCleanLog(hostRun.LogPath, message+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: message}
	}
	unitName, logPath, err := o.SelfUpdater.Schedule(ctx, hostRun.RunID)
	if err != nil {
		if errors.Is(err, selfupdate.ErrUnsupported) {
			message := "self-update not supported on macOS development environment"
			appendCleanLog(hostRun.LogPath, message+"\n")
			return hostOutcome{Status: HostRunStatusSkipped, ExitCode: 0, Summary: message}
		}
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	message := fmt.Sprintf("self-update scheduled, see %s", filepath.Base(logPath))
	appendCleanLog(hostRun.LogPath, fmt.Sprintf("scheduled %s; output: %s\n", unitName, logPath))
	return hostOutcome{Status: HostRunStatusSuccess, ExitCode: 0, Summary: message}
}

func (o *Orchestrator) executeTrueNASRun(ctx context.Context, host Host, hostRun HostRun) hostOutcome {
	appendCleanLog(hostRun.LogPath, fmt.Sprintf("patchpilot TrueNAS host %s started at %s\n", host.Name, time.Now().UTC().Format(time.RFC3339)))
	token, err := o.decryptHostAPIToken(ctx, host.ID)
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	client := truenas.New(host.Hostname, token)
	info, err := client.Verify(ctx)
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	appendCleanLog(hostRun.LogPath, "TrueNAS SCALE version: "+info.Version+"\n")
	result, err := client.ApplyUpdate(ctx, func(line string) { appendCleanLog(hostRun.LogPath, line) })
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	packages := 0
	if result.Applied {
		packages = 1
	}
	return hostOutcome{Status: HostRunStatusSuccess, ExitCode: 0, Packages: packages, RebootRequired: result.RebootRequired, Summary: result.Summary}
}

func (o *Orchestrator) executeHAOSRun(ctx context.Context, host Host, hostRun HostRun) hostOutcome {
	appendCleanLog(hostRun.LogPath, fmt.Sprintf("patchpilot HAOS host %s started at %s\n", host.Name, time.Now().UTC().Format(time.RFC3339)))
	token, err := o.decryptHostAPIToken(ctx, host.ID)
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	client := haos.New(host.Hostname, token)
	info, err := client.Verify(ctx)
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	appendCleanLog(hostRun.LogPath, "HAOS Supervisor version: "+info.Supervisor.Version+"\n")
	appendCleanLog(hostRun.LogPath, "HAOS Core version: "+info.Core.Version+"\n")
	result, err := client.ApplyUpdates(ctx, func(line string) { appendCleanLog(hostRun.LogPath, line) })
	if err != nil {
		appendCleanLog(hostRun.LogPath, err.Error()+"\n")
		return hostOutcome{Status: HostRunStatusFailed, ExitCode: -1, Summary: err.Error()}
	}
	return hostOutcome{Status: HostRunStatusSuccess, ExitCode: 0, Packages: len(result.Updated), RebootRequired: false, Summary: result.Summary}
}

func (o *Orchestrator) decryptHostAPIToken(ctx context.Context, hostID int64) (string, error) {
	if o.Secrets == nil {
		return "", fmt.Errorf("API token encryption is not configured")
	}
	tokenStore, ok := o.Hosts.(HostAPITokenRepository)
	if !ok {
		return "", fmt.Errorf("host token storage is not configured")
	}
	encrypted, err := tokenStore.GetAPIToken(ctx, hostID)
	if err != nil {
		return "", err
	}
	return o.Secrets.DecryptString(ctx, encrypted.Ciphertext, encrypted.Nonce)
}

func (o *Orchestrator) checkRebootRequired(ctx context.Context, runner remotessh.Runner, manager pkgmgr.PackageManager, paths map[string]string, logPath string) bool {
	command, err := pkgmgr.RebootRequiredCommand(manager, paths)
	if err != nil {
		appendCleanLog(logPath, "reboot detection skipped: "+err.Error()+"\n")
		return false
	}
	result, err := runner.Run(ctx, command, nil)
	if err != nil && result.ExitCode != 1 {
		appendCleanLog(logPath, "reboot detection failed: "+err.Error()+"\n")
	}
	if result.ExitCode == 0 {
		appendCleanLog(logPath, "reboot required\n")
		return true
	}
	appendCleanLog(logPath, "reboot not required\n")
	return false
}

func (o *Orchestrator) publish(runID int64, event RunEvent) {
	if o.Streams == nil {
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	o.Streams.Ensure(RunStreamID(runID))
	_ = o.Streams.Publish(RunStreamID(runID), "run.event", event)
}

func (o *Orchestrator) audit(ctx context.Context, actor string, action string, targetType string, targetID *int64, details map[string]any) {
	if o.Audit == nil {
		return
	}
	payload, err := json.Marshal(details)
	if err != nil {
		payload = []byte("{}")
	}
	if err := o.Audit.Create(ctx, AuditEvent{Actor: actor, Action: action, TargetType: targetType, TargetID: targetID, Details: string(payload)}); err != nil && o.Logger != nil {
		o.Logger.Error("create audit event", "error", err)
	}
}

func orderHosts(hosts []Host) []Host {
	groups := make([][]Host, 5)
	for _, host := range hosts {
		switch {
		case host.Type == HostTypeTrueNAS || pkgmgr.PackageManager(host.PackageManager) == pkgmgr.ManagerTrueNASAPI:
			groups[1] = append(groups[1], host)
		case host.Type == HostTypeHAOS || pkgmgr.PackageManager(host.PackageManager) == pkgmgr.ManagerHAOSAPI:
			groups[2] = append(groups[2], host)
		case host.IsProxmoxHost:
			groups[3] = append(groups[3], host)
		case host.IsSelf:
			groups[4] = append(groups[4], host)
		default:
			groups[0] = append(groups[0], host)
		}
	}
	ordered := make([]Host, 0, len(hosts))
	for _, group := range groups {
		ordered = append(ordered, group...)
	}
	return ordered
}

func appendCleanLog(path string, body string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_, _ = file.WriteString(stripANSI(body))
}

type hostOutcome struct {
	Status         HostRunStatus
	ExitCode       int
	Packages       int
	RebootRequired bool
	Summary        string
}

type ansiStripper struct {
	inEscape bool
}

func (s *ansiStripper) Clean(value string) string {
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		char := value[index]
		if s.inEscape {
			if char >= '@' && char <= '~' {
				s.inEscape = false
			}
			continue
		}
		if char == 0x1b {
			s.inEscape = true
			continue
		}
		builder.WriteByte(char)
	}
	return builder.String()
}
