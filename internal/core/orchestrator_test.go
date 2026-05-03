package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOrderHostsGroupOrder(t *testing.T) {
	t.Parallel()
	hosts := []Host{
		{ID: 5, Name: "self", Type: HostTypeSelf, IsSelf: true},
		{ID: 4, Name: "pve", Type: HostTypeStandard, IsProxmoxHost: true},
		{ID: 2, Name: "nas", Type: HostTypeTrueNAS},
		{ID: 1, Name: "vm", Type: HostTypeStandard},
		{ID: 3, Name: "ha", Type: HostTypeHAOS},
	}
	ordered := orderHosts(hosts)
	want := []int64{1, 2, 3, 4, 5}
	for index, host := range ordered {
		if host.ID != want[index] {
			t.Fatalf("ordered[%d] = %d, want %d", index, host.ID, want[index])
		}
	}
}

func TestOrchestratorOverlapPolicies(t *testing.T) {
	t.Parallel()
	hosts := &fakeHostStore{hosts: map[int64]Host{1: {ID: 1, Name: "vm", Type: HostTypeStandard, Enabled: true}}}
	runs := &fakeRunStore{active: &Run{ID: 99, Status: RunStatusRunning}}
	schedules := &fakeScheduleStore{schedule: Schedule{ID: 7, Name: "weekly", HostIDs: []int64{1}, Enabled: true}}
	audit := &fakeAuditStore{}
	orchestrator := NewOrchestrator(hosts, runs, schedules, audit, nil, nil, nil)

	detail, err := orchestrator.RunHostNow(context.Background(), 1, RunTriggerManual, "tui")
	if err != nil {
		t.Fatalf("RunHostNow() error: %v", err)
	}
	if detail.Run.Status != RunStatusQueued {
		t.Fatalf("manual overlap status = %q, want queued", detail.Run.Status)
	}

	_, err = orchestrator.RunSchedule(context.Background(), 7, RunTriggerScheduled, "scheduler", nil)
	if !errors.Is(err, ErrScheduledOverlap) {
		t.Fatalf("RunSchedule() error = %v, want ErrScheduledOverlap", err)
	}
	if !schedules.skipped {
		t.Fatalf("scheduled overlap did not mark schedule skipped")
	}
	if audit.lastAction != "run.skipped_overlap" {
		t.Fatalf("audit action = %q, want run.skipped_overlap", audit.lastAction)
	}
}

type fakeHostStore struct {
	hosts map[int64]Host
}

func (s *fakeHostStore) List(context.Context) ([]Host, error) { return nil, nil }
func (s *fakeHostStore) Get(_ context.Context, id int64) (Host, error) {
	return s.hosts[id], nil
}
func (s *fakeHostStore) Create(context.Context, HostCreate) (Host, error) { return Host{}, nil }
func (s *fakeHostStore) Update(context.Context, int64, HostUpdate) (Host, error) {
	return Host{}, nil
}
func (s *fakeHostStore) SetEnabled(context.Context, int64, bool) (Host, error) { return Host{}, nil }
func (s *fakeHostStore) Delete(context.Context, int64) error                   { return nil }

type fakeRunStore struct {
	active *Run
	nextID int64
}

func (s *fakeRunStore) CreateRunWithHosts(_ context.Context, input RunCreate, hostIDs []int64) (RunDetail, error) {
	s.nextID++
	run := Run{ID: s.nextID, Trigger: input.Trigger, ScheduleID: input.ScheduleID, Status: input.Status, Summary: input.Summary, StartedAt: time.Now().UTC()}
	hostRuns := make([]HostRun, 0, len(hostIDs))
	for index, hostID := range hostIDs {
		hostRuns = append(hostRuns, HostRun{ID: int64(index + 1), RunID: run.ID, HostID: hostID, Status: HostRunStatusPending})
	}
	return RunDetail{Run: run, HostRuns: hostRuns}, nil
}
func (s *fakeRunStore) List(context.Context) ([]Run, error)            { return nil, nil }
func (s *fakeRunStore) Get(context.Context, int64) (RunDetail, error)  { return RunDetail{}, nil }
func (s *fakeRunStore) Active(context.Context) (*Run, error)           { return s.active, nil }
func (s *fakeRunStore) NextQueued(context.Context) (*RunDetail, error) { return nil, nil }
func (s *fakeRunStore) PromoteQueued(context.Context, int64) (RunDetail, error) {
	return RunDetail{}, nil
}
func (s *fakeRunStore) StartHostRun(context.Context, int64) error { return nil }
func (s *fakeRunStore) FinishHostRun(context.Context, int64, HostRunStatus, int, int, bool, string) error {
	return nil
}
func (s *fakeRunStore) FinishRun(context.Context, int64, RunStatus, string) error { return nil }
func (s *fakeRunStore) CancelRun(context.Context, int64, string) error            { return nil }

type fakeScheduleStore struct {
	schedule Schedule
	skipped  bool
}

func (s *fakeScheduleStore) List(context.Context) ([]Schedule, error) {
	return []Schedule{s.schedule}, nil
}
func (s *fakeScheduleStore) ListEnabled(context.Context) ([]Schedule, error) {
	return []Schedule{s.schedule}, nil
}
func (s *fakeScheduleStore) Get(context.Context, int64) (Schedule, error) { return s.schedule, nil }
func (s *fakeScheduleStore) Create(context.Context, ScheduleInput, *time.Time) (Schedule, error) {
	return s.schedule, nil
}
func (s *fakeScheduleStore) Update(context.Context, int64, ScheduleInput, *time.Time) (Schedule, error) {
	return s.schedule, nil
}
func (s *fakeScheduleStore) Delete(context.Context, int64) error { return nil }
func (s *fakeScheduleStore) MarkFired(context.Context, int64, time.Time, *time.Time) error {
	return nil
}
func (s *fakeScheduleStore) MarkSkipped(context.Context, int64, time.Time, string, *time.Time) error {
	s.skipped = true
	return nil
}

type fakeAuditStore struct {
	lastAction string
}

func (s *fakeAuditStore) Create(_ context.Context, event AuditEvent) error {
	s.lastAction = event.Action
	return nil
}
