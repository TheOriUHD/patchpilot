package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
)

func TestConfirmPendingDismissesBeforeReturningCommand(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), "/tmp/patchpilot.sock")
	m.width = 120
	m.height = 40
	m.daemonState = components.DaemonUp
	m.hosts = []core.Host{{ID: 42, Name: "debian", Type: core.HostTypeStandard, Enabled: true}}

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if cmd != nil {
		t.Fatal("opening confirmation should not return command")
	}
	confirming := updated.(model)
	if !strings.Contains(confirming.View(), "Run updates now on debian? y/n") {
		t.Fatalf("confirmation modal not visible in rendered view")
	}

	updated, cmd = confirming.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	result := updated.(model)
	if result.pending.Kind != "" {
		t.Fatalf("pending action still visible after confirm: %#v", result.pending)
	}
	if strings.Contains(result.View(), "Run updates now on debian? y/n") {
		t.Fatalf("confirmation modal still rendered after confirm")
	}
	if cmd == nil {
		t.Fatal("confirming run should return an action command")
	}
}

func TestOpenLiveRunClearsStaleConfirmations(t *testing.T) {
	t.Parallel()

	m := liveRunTestModel()
	m.pending = pendingAction{Kind: "run", Host: core.Host{ID: 1, Name: "debian"}}
	m.schedulePending = schedulePendingAction{Kind: "delete", ID: 1, Name: "weekly"}
	m.settingsConfirm = "rotate-key"
	m.liveCancel = true
	m.openLiveRun(core.RunDetail{Run: core.Run{ID: 2, Trigger: core.RunTriggerManual, Status: core.RunStatusRunning, StartedAt: time.Now()}, HostRuns: []core.HostRun{{ID: 20, RunID: 2, HostID: 1, HostName: "debian", Status: core.HostRunStatusRunning}}})

	if m.pending.Kind != "" || m.schedulePending.Kind != "" || m.settingsConfirm != "" || m.liveCancel {
		t.Fatalf("confirmations were not cleared: pending=%#v schedule=%#v settings=%q liveCancel=%v", m.pending, m.schedulePending, m.settingsConfirm, m.liveCancel)
	}
}

func TestHostDetailRepairSudoersAction(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), "/tmp/patchpilot.sock")
	m.daemonState = components.DaemonUp
	m.hosts = []core.Host{{ID: 7, Name: "debian", Type: core.HostTypeStandard, Enabled: true}}
	m.hostScreen = hostScreenDetail

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	result := updated.(model)
	if cmd != nil {
		t.Fatal("repair confirmation should not immediately return command")
	}
	if result.pending.Kind != "repair-sudoers" {
		t.Fatalf("pending kind = %q, want repair-sudoers", result.pending.Kind)
	}
}

func TestLiveRunModalMinimizeAndRestore(t *testing.T) {
	t.Parallel()

	m := liveRunTestModel()
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	minimized := updated.(model)
	if minimized.liveModalVisible || !minimized.liveMinimized {
		t.Fatalf("after Esc visible=%v minimized=%v, want hidden/minimized", minimized.liveModalVisible, minimized.liveMinimized)
	}
	if !strings.Contains(minimized.runBanner, "press L to view") {
		t.Fatalf("run banner = %q, want live restore hint", minimized.runBanner)
	}

	updated, _ = minimized.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	restored := updated.(model)
	if !restored.liveModalVisible || restored.liveMinimized {
		t.Fatalf("after L visible=%v minimized=%v, want visible/not minimized", restored.liveModalVisible, restored.liveMinimized)
	}
}

func TestLiveRunCompletionState(t *testing.T) {
	t.Parallel()

	m := liveRunTestModel()
	cmd := m.applyRunEvent(core.RunEvent{RunID: 1, RunStatus: core.RunStatusSuccess, Message: "1 succeeded, 0 failed", OccurredAt: time.Now()})
	if cmd != nil {
		t.Fatalf("auto-reboot command = %v, want nil when host policy is report-only", cmd)
	}
	if !m.liveCompleted {
		t.Fatal("liveCompleted = false, want true")
	}
	if got := m.liveLogLines[len(m.liveLogLines)-1].line; got != "FINISHED" {
		t.Fatalf("last log line = %q, want FINISHED", got)
	}
	if summary := m.liveCompletionSummary(); !strings.Contains(summary, "Completed") {
		t.Fatalf("completion summary = %q, want Completed", summary)
	}
}

func TestAutoRebootCountdownCancellation(t *testing.T) {
	t.Parallel()

	m := liveRunTestModel()
	m.hosts[0].AutoReboot = true
	m.hosts[0].AutoRebootDelay = 30
	m.liveRun.HostRuns[0].Status = core.HostRunStatusSuccess
	m.liveRun.HostRuns[0].RebootRequired = true

	cmd := m.applyRunEvent(core.RunEvent{RunID: 1, RunStatus: core.RunStatusSuccess, Message: "done", OccurredAt: time.Now()})
	if cmd == nil {
		t.Fatal("completion should start auto-reboot countdown")
	}
	if !m.autoReboot.active || m.autoReboot.secondsLeft != 30 {
		t.Fatalf("auto reboot state = %#v, want active 30s countdown", m.autoReboot)
	}

	updated, _ := m.handleLiveRunKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	cancelled := updated.(model)
	if cancelled.autoReboot.active {
		t.Fatal("auto reboot still active after cancellation")
	}
	if cancelled.autoReboot.cancelledHostID != 1 {
		t.Fatalf("cancelledHostID = %d, want 1", cancelled.autoReboot.cancelledHostID)
	}
}

func liveRunTestModel() model {
	model := newModel(context.Background(), "/tmp/patchpilot.sock")
	model.width = 120
	model.height = 40
	model.daemonState = components.DaemonUp
	model.hosts = []core.Host{{ID: 1, Name: "debian", Type: core.HostTypeStandard, Enabled: true, AutoRebootDelay: 30}}
	model.openLiveRun(core.RunDetail{Run: core.Run{ID: 1, Trigger: core.RunTriggerManual, Status: core.RunStatusRunning, StartedAt: time.Now()}, HostRuns: []core.HostRun{{ID: 10, RunID: 1, HostID: 1, HostName: "debian", Status: core.HostRunStatusRunning}}})
	return model
}
