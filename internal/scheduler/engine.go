package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/TheOriUHD/patchpilot/internal/core"
)

// ScheduleStore loads enabled schedules for the engine.
type ScheduleStore interface {
	ListEnabled(ctx context.Context) ([]core.Schedule, error)
}

// RunOrchestrator starts scheduled runs.
type RunOrchestrator interface {
	RunSchedule(ctx context.Context, scheduleID int64, trigger core.RunTrigger, actor string, nextRunAt *time.Time) (core.RunDetail, error)
}

// Engine owns cron jobs for enabled schedules.
type Engine struct {
	store        ScheduleStore
	orchestrator RunOrchestrator
	logger       *slog.Logger

	mu      sync.Mutex
	running bool
	sched   gocron.Scheduler
}

// NewEngine creates a scheduler engine.
func NewEngine(store ScheduleStore, orchestrator RunOrchestrator, logger *slog.Logger) *Engine {
	return &Engine{store: store, orchestrator: orchestrator, logger: logger}
}

// Start loads jobs and starts scheduling until ctx is cancelled.
func (e *Engine) Start(ctx context.Context) error {
	if err := e.Reload(ctx); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		if err := e.Shutdown(); err != nil && e.logger != nil {
			e.logger.Error("shutdown scheduler", "error", err)
		}
	}()
	return nil
}

// Reload replaces all schedule jobs with the currently enabled schedules.
func (e *Engine) Reload(ctx context.Context) error {
	schedules, err := e.store.ListEnabled(ctx)
	if err != nil {
		return err
	}
	newScheduler, err := gocron.NewScheduler()
	if err != nil {
		return fmt.Errorf("create scheduler: %w", err)
	}
	for _, schedule := range schedules {
		expr, err := CronExpression(InputFromSchedule(schedule))
		if err != nil {
			return fmt.Errorf("schedule %s cron expression: %w", schedule.Name, err)
		}
		scheduleCopy := schedule
		_, err = newScheduler.NewJob(gocron.CronJob(expr, false), gocron.NewTask(func() {
			e.fire(scheduleCopy)
		}), gocron.WithName(fmt.Sprintf("schedule-%d", schedule.ID)))
		if err != nil {
			return fmt.Errorf("register schedule %s: %w", schedule.Name, err)
		}
	}
	newScheduler.Start()

	e.mu.Lock()
	oldScheduler := e.sched
	e.sched = newScheduler
	e.running = true
	e.mu.Unlock()
	if oldScheduler != nil {
		if err := oldScheduler.Shutdown(); err != nil {
			return fmt.Errorf("shutdown old scheduler: %w", err)
		}
	}
	return nil
}

// Shutdown stops the current scheduler.
func (e *Engine) Shutdown() error {
	e.mu.Lock()
	scheduler := e.sched
	e.sched = nil
	e.running = false
	e.mu.Unlock()
	if scheduler == nil {
		return nil
	}
	return scheduler.Shutdown()
}

func (e *Engine) fire(schedule core.Schedule) {
	now := time.Now().UTC()
	nextRuns, err := NextRuns(InputFromSchedule(schedule), now.Add(time.Second), 1)
	if err != nil {
		if e.logger != nil {
			e.logger.Error("compute next scheduled run", "error", err, "schedule_id", schedule.ID)
		}
		return
	}
	var nextRunAt *time.Time
	if len(nextRuns) > 0 {
		nextRunAt = &nextRuns[0]
	}
	_, err = e.orchestrator.RunSchedule(context.Background(), schedule.ID, core.RunTriggerScheduled, "scheduler", nextRunAt)
	if err != nil && !errors.Is(err, core.ErrScheduledOverlap) && e.logger != nil {
		e.logger.Error("start scheduled run", "error", err, "schedule_id", schedule.ID)
	}
}
