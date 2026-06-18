package core

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
	golangssh "golang.org/x/crypto/ssh"
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

func TestConcurrencyGating(t *testing.T) {
	t.Parallel()
	scheduleID := int64(12)
	orchestrator := &Orchestrator{
		Schedules: &fakeScheduleStore{schedule: Schedule{ID: scheduleID, Name: "weekly", MaxConcurrent: 7}},
		Settings:  fakeSettingsStore{concurrency: true},
	}
	got := orchestrator.maxConcurrentForRun(context.Background(), RunDetail{Run: Run{ScheduleID: &scheduleID}})
	if got != 7 {
		t.Fatalf("maxConcurrentForRun() = %d, want 7", got)
	}
	orchestrator.Settings = fakeSettingsStore{concurrency: false}
	got = orchestrator.maxConcurrentForRun(context.Background(), RunDetail{Run: Run{ScheduleID: &scheduleID}})
	if got != 1 {
		t.Fatalf("maxConcurrentForRun() with setting off = %d, want 1", got)
	}
	groups := map[string]int{
		"standard": hostExecutionGroup(Host{Type: HostTypeStandard}),
		"truenas":  hostExecutionGroup(Host{Type: HostTypeTrueNAS}),
		"haos":     hostExecutionGroup(Host{Type: HostTypeHAOS}),
		"proxmox":  hostExecutionGroup(Host{Type: HostTypeStandard, IsProxmoxHost: true}),
		"self":     hostExecutionGroup(Host{Type: HostTypeSelf, IsSelf: true}),
	}
	want := map[string]int{"standard": 0, "truenas": 1, "haos": 2, "proxmox": 3, "self": 4}
	for name, group := range groups {
		if group != want[name] {
			t.Fatalf("hostExecutionGroup(%s) = %d, want %d", name, group, want[name])
		}
	}
}

func TestSettingsServiceConcurrencyAudit(t *testing.T) {
	t.Parallel()
	settings := &fakeSettingsStore{}
	audit := &fakeAuditStore{}
	service := SettingsService{Settings: settings, Audit: audit}
	if err := service.SetConcurrency(context.Background(), true); err != nil {
		t.Fatalf("SetConcurrency() error: %v", err)
	}
	if !settings.concurrency {
		t.Fatalf("concurrency setting was not updated")
	}
	if audit.lastAction != "settings.concurrency_changed" {
		t.Fatalf("audit action = %q, want settings.concurrency_changed", audit.lastAction)
	}
}

func TestSettingsServiceRotateKeypairSuccess(t *testing.T) {
	t.Parallel()
	keyStore := remotessh.NewKeyStore(t.TempDir())
	oldKey, err := keyStore.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}
	audit := &fakeAuditStore{}
	service := SettingsService{
		Hosts: &fakeHostStore{hosts: map[int64]Host{
			1: {ID: 1, Name: "vm1", Hostname: "vm1", SSHUser: "patchpilot", Type: HostTypeStandard, Enabled: true},
			2: {ID: 2, Name: "nas", Hostname: "nas", Type: HostTypeTrueNAS, Enabled: true},
		}},
		Audit: audit,
		Keys:  keyStore,
	}
	service.ConnectKey = func(context.Context, string, int, string, golangssh.Signer) (remotessh.Runner, error) {
		return &fakeSSHRunner{}, nil
	}
	if err := service.RotateKeypair(context.Background()); err != nil {
		t.Fatalf("RotateKeypair() error: %v", err)
	}
	newKey, err := keyStore.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() after rotate error: %v", err)
	}
	if newKey.PublicKey == oldKey.PublicKey {
		t.Fatalf("public key was not replaced")
	}
	if audit.lastAction != "settings.key_rotation_success" {
		t.Fatalf("audit action = %q, want settings.key_rotation_success", audit.lastAction)
	}
}

func TestSettingsServiceRotateKeypairAbortKeepsOldKey(t *testing.T) {
	t.Parallel()
	keyStore := remotessh.NewKeyStore(t.TempDir())
	oldKey, err := keyStore.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}
	audit := &fakeAuditStore{}
	service := SettingsService{
		Hosts: &fakeHostStore{hosts: map[int64]Host{
			1: {ID: 1, Name: "vm1", Hostname: "vm1", SSHUser: "patchpilot", Type: HostTypeStandard, Enabled: true},
			2: {ID: 2, Name: "vm2", Hostname: "vm2", SSHUser: "patchpilot", Type: HostTypeStandard, Enabled: true},
		}},
		Audit: audit,
		Keys:  keyStore,
	}
	service.ConnectKey = func(_ context.Context, host string, _ int, _ string, signer golangssh.Signer) (remotessh.Runner, error) {
		if host == "vm2" && !bytes.Equal(signer.PublicKey().Marshal(), oldKey.Signer.PublicKey().Marshal()) {
			return nil, errors.New("new key rejected")
		}
		return &fakeSSHRunner{}, nil
	}
	if err := service.RotateKeypair(context.Background()); err == nil {
		t.Fatalf("RotateKeypair() error = nil, want abort")
	}
	currentKey, err := keyStore.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() after abort error: %v", err)
	}
	if currentKey.PublicKey != oldKey.PublicKey {
		t.Fatalf("public key changed after aborted rotation")
	}
	if audit.lastAction != "settings.key_rotation_abort" {
		t.Fatalf("audit action = %q, want settings.key_rotation_abort", audit.lastAction)
	}
}

func TestHostServiceRepairSudoersIdempotent(t *testing.T) {
	t.Parallel()

	keyStore := remotessh.NewKeyStore(t.TempDir())
	runner := &fakeSSHRunner{responses: map[string]fakeSSHResponse{
		"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: aptResolvedPathsOutput()}},
	}}
	service := NewHostService(&fakeHostStore{hosts: map[int64]Host{1: {ID: 1, Name: "vm1", Hostname: "vm1", SSHUser: "patchpilot", Type: HostTypeStandard, PackageManager: "apt", Enabled: true}}}, nil, keyStore, nil, nil)
	service.ConnectKey = func(context.Context, string, int, string, golangssh.Signer) (remotessh.Runner, error) {
		return runner, nil
	}

	if err := service.RepairSudoers(context.Background(), 1); err != nil {
		t.Fatalf("RepairSudoers() error: %v", err)
	}
	if err := service.RepairSudoers(context.Background(), 1); err != nil {
		t.Fatalf("RepairSudoers() second run error: %v", err)
	}
	if got := runner.countContaining("cat > /etc/sudoers.d/patchpilot"); got != 2 {
		t.Fatalf("sudoers write count = %d, want 2 idempotent rewrites", got)
	}
	for _, want := range []string{"/usr/bin/systemctl reboot", "/usr/sbin/reboot", "/usr/sbin/shutdown -r now"} {
		if !runner.anyCommandContaining(want) {
			t.Fatalf("repair command missing %q; commands=%#v", want, runner.commands)
		}
	}
}

func TestHostServiceRebootFriendlySudoError(t *testing.T) {
	t.Parallel()

	keyStore := remotessh.NewKeyStore(t.TempDir())
	runner := &fakeSSHRunner{responses: map[string]fakeSSHResponse{
		"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: "systemctl\t/usr/bin/systemctl\nreboot\t/usr/sbin/reboot\nshutdown\t/usr/sbin/shutdown\n"}},
		"systemctl reboot":           {result: remotessh.CommandResult{Stderr: "sudo: a password is required", ExitCode: 1}, err: errors.New("Process exited with status 1")},
	}}
	service := NewHostService(&fakeHostStore{hosts: map[int64]Host{1: {ID: 1, Name: "frigate", Hostname: "frigate", SSHUser: "patchpilot", Type: HostTypeStandard, PackageManager: "apt", Enabled: true}}}, nil, keyStore, nil, nil)
	service.ConnectKey = func(context.Context, string, int, string, golangssh.Signer) (remotessh.Runner, error) {
		return runner, nil
	}

	err := service.RebootHost(context.Background(), 1)
	if err == nil {
		t.Fatal("RebootHost() expected error")
	}
	if !strings.Contains(err.Error(), "lacks NOPASSWD sudo for reboot commands") || !strings.Contains(err.Error(), "Press Shift+P") {
		t.Fatalf("RebootHost() error = %q, want friendly repair hint", err)
	}
}

func TestReconnectPatchpilotRetries(t *testing.T) {
	t.Parallel()

	keypair, err := remotessh.GenerateInMemory()
	if err != nil {
		t.Fatalf("GenerateInMemory() error: %v", err)
	}
	service := NewHostService(nil, nil, nil, nil, nil)
	attempts := 0
	service.ConnectKey = func(context.Context, string, int, string, golangssh.Signer) (remotessh.Runner, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("not ready")
		}
		return &fakeSSHRunner{}, nil
	}
	reporter := &collectingBootstrapReporter{}
	runner, err := service.reconnectPatchpilotWithRetry(context.Background(), "host", 22, keypair, reporter, []time.Duration{0, 0, 0})
	if err != nil {
		t.Fatalf("reconnectPatchpilotWithRetry() error: %v", err)
	}
	if runner == nil || attempts != 3 {
		t.Fatalf("runner=%#v attempts=%d, want runner after 3 attempts", runner, attempts)
	}
	if len(reporter.events) != 3 || !strings.Contains(reporter.events[1].Message, "attempt 2/4") {
		t.Fatalf("events = %#v, want attempt progress", reporter.events)
	}
}

func TestReconnectPatchpilotHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	keypair, err := remotessh.GenerateInMemory()
	if err != nil {
		t.Fatalf("GenerateInMemory() error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := NewHostService(nil, nil, nil, nil, nil)
	service.ConnectKey = func(context.Context, string, int, string, golangssh.Signer) (remotessh.Runner, error) {
		cancel()
		return nil, errors.New("not ready")
	}
	_, err = service.reconnectPatchpilotWithRetry(ctx, "host", 22, keypair, nil, []time.Duration{time.Hour})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
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

func TestScheduleSuppressRebootOverridesHostPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		suppressReboot  bool
		hostAutoReboot  bool
		scheduleBacked  bool
		wantReboot      bool
		wantRebootProbe bool
	}{
		{name: "schedule suppress on host would reboot", suppressReboot: true, hostAutoReboot: true, scheduleBacked: true, wantReboot: false, wantRebootProbe: false},
		{name: "schedule suppress off host policy honored", suppressReboot: false, hostAutoReboot: true, scheduleBacked: true, wantReboot: true, wantRebootProbe: true},
		{name: "schedule suppress on host never reboot", suppressReboot: true, hostAutoReboot: false, scheduleBacked: true, wantReboot: false, wantRebootProbe: false},
		{name: "manual ad hoc unaffected", suppressReboot: true, hostAutoReboot: true, scheduleBacked: false, wantReboot: true, wantRebootProbe: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			scheduleID := int64(7)
			run := Run{ID: 1, Trigger: RunTriggerManual, Status: RunStatusRunning, StartedAt: time.Now().UTC()}
			if test.scheduleBacked {
				run.ScheduleID = &scheduleID
			}
			hostRun := HostRun{ID: 11, RunID: run.ID, HostID: 1, Status: HostRunStatusPending, LogPath: filepath.Join(t.TempDir(), "host.log")}
			runner := &fakeSSHRunner{responses: map[string]fakeSSHResponse{
				"resolve_patchpilot_command":                {result: remotessh.CommandResult{Stdout: aptResolvedPathsOutput()}},
				"/usr/bin/test -f /var/run/reboot-required": {result: remotessh.CommandResult{ExitCode: 0}},
			}}
			runs := &fakeRunStore{}
			orchestrator := &Orchestrator{
				Hosts:     &fakeHostStore{hosts: map[int64]Host{1: {ID: 1, Name: "vm", Hostname: "vm", SSHUser: "patchpilot", Type: HostTypeStandard, PackageManager: "apt", Enabled: true, AutoReboot: test.hostAutoReboot}}},
				Runs:      runs,
				Schedules: &fakeScheduleStore{schedule: Schedule{ID: scheduleID, Name: "weekly", HostIDs: []int64{1}, Enabled: true, SuppressReboot: test.suppressReboot}},
				Keys:      testKeyStore(t),
			}
			orchestrator.ConnectKey = func(context.Context, string, int, string, golangssh.Signer) (standardHostRunner, error) {
				return runner, nil
			}

			success, failure, skipped, cancelled := orchestrator.executeRunTasks(context.Background(), RunDetail{Run: run, HostRuns: []HostRun{hostRun}})
			if success != 1 || failure != 0 || skipped != 0 || cancelled {
				t.Fatalf("counts success=%d failure=%d skipped=%d cancelled=%v", success, failure, skipped, cancelled)
			}
			if runs.lastRebootRequired != test.wantReboot {
				t.Fatalf("rebootRequired = %v, want %v", runs.lastRebootRequired, test.wantReboot)
			}
			if got := runner.anyCommandContaining("/usr/bin/test -f /var/run/reboot-required"); got != test.wantRebootProbe {
				t.Fatalf("reboot probe executed = %v, want %v; commands=%#v", got, test.wantRebootProbe, runner.commands)
			}
		})
	}
}

type fakeHostStore struct {
	hosts map[int64]Host
}

func (s *fakeHostStore) List(context.Context) ([]Host, error) {
	hosts := make([]Host, 0, len(s.hosts))
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	return hosts, nil
}
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
	active             *Run
	nextID             int64
	lastRebootRequired bool
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

func (s *fakeRunStore) FinishHostRun(_ context.Context, _ int64, _ HostRunStatus, _ int, _ int, rebootRequired bool, _ string) error {
	s.lastRebootRequired = rebootRequired
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

type fakeSettingsStore struct {
	concurrency bool
}

type collectingBootstrapReporter struct {
	events []BootstrapEvent
}

func (r *collectingBootstrapReporter) Report(event BootstrapEvent) {
	r.events = append(r.events, event)
}

func (s fakeSettingsStore) GetBool(context.Context, string, bool) (bool, error) {
	return s.concurrency, nil
}

func (s *fakeSettingsStore) SetBool(_ context.Context, _ string, value bool) error {
	s.concurrency = value
	return nil
}

func (s fakeSettingsStore) ConcurrencyEnabled(context.Context) (bool, error) {
	return s.concurrency, nil
}

func testKeyStore(t *testing.T) *remotessh.KeyStore {
	t.Helper()
	store := remotessh.NewKeyStore(t.TempDir())
	if _, err := store.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}
	return store
}

type fakeSSHResponse struct {
	result remotessh.CommandResult
	err    error
}

type fakeSSHRunner struct {
	commands  []string
	responses map[string]fakeSSHResponse
}

func (r *fakeSSHRunner) Run(_ context.Context, command string, _ []byte) (remotessh.CommandResult, error) {
	r.commands = append(r.commands, command)
	for needle, response := range r.responses {
		if strings.Contains(command, needle) {
			return response.result, response.err
		}
	}
	return remotessh.CommandResult{}, nil
}

func (r *fakeSSHRunner) RunStream(_ context.Context, command string, _ []byte, onChunk func(remotessh.StreamChunk)) (remotessh.CommandResult, error) {
	r.commands = append(r.commands, command)
	if onChunk != nil {
		onChunk(remotessh.StreamChunk{Stream: "stdout", Data: []byte("updated\n")})
	}
	for needle, response := range r.responses {
		if strings.Contains(command, needle) {
			return response.result, response.err
		}
	}
	return remotessh.CommandResult{}, nil
}

func (r *fakeSSHRunner) Close() error { return nil }

func (r *fakeSSHRunner) countContaining(needle string) int {
	count := 0
	for _, command := range r.commands {
		if strings.Contains(command, needle) {
			count++
		}
	}
	return count
}

func (r *fakeSSHRunner) anyCommandContaining(needle string) bool {
	return r.countContaining(needle) > 0
}
