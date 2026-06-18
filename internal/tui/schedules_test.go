package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
)

func TestScheduleEditorConcurrencyFieldGating(t *testing.T) {
	t.Parallel()
	withoutSetting := model{}
	if hasField(withoutSetting.scheduleEditorFields(), "Max") {
		t.Fatalf("Max field shown while concurrency setting is absent")
	}
	off := model{settingsSnapshot: &core.SettingsSnapshot{ConcurrencyEnabled: false}}
	if hasField(off.scheduleEditorFields(), "Max") {
		t.Fatalf("Max field shown while concurrency is off")
	}
	on := model{settingsSnapshot: &core.SettingsSnapshot{ConcurrencyEnabled: true}}
	if !hasField(on.scheduleEditorFields(), "Max") {
		t.Fatalf("Max field hidden while concurrency is on")
	}
}

func TestScheduleEditorSuppressRebootToggleRendersAndFlips(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), "/tmp/patchpilot.sock")
	m.width = 120
	m.height = 40
	m.daemonState = components.DaemonUp
	m.scheduleScreen = scheduleScreenEditor
	m.scheduleEditor = scheduleEditor{input: core.ScheduleInput{Name: "weekly", Frequency: core.FrequencyWeekly, Hour: 3, DayOfWeek: 0, Enabled: true}}
	m.scheduleEditor.field = fieldIndex(t, m.scheduleEditorFields(), "Reboot")

	view := ansiPattern.ReplaceAllString(m.renderScheduleEditor(30), "")
	if !strings.Contains(view, "[ ] Suppress automatic reboot for this schedule") {
		t.Fatalf("rendered schedule editor missing suppress toggle:\n%s", view)
	}
	if !strings.Contains(view, "reboots are suppressed, never forced") {
		t.Fatalf("rendered schedule editor missing one-way suppressor note:\n%s", view)
	}

	updated, _ := m.handleScheduleEditorKey(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	if !m.scheduleEditor.input.SuppressReboot {
		t.Fatal("SuppressReboot = false after Space on Reboot field, want true")
	}
	view = ansiPattern.ReplaceAllString(m.renderScheduleEditor(30), "")
	if !strings.Contains(view, "[x] Suppress automatic reboot for this schedule") {
		t.Fatalf("rendered schedule editor missing checked suppress toggle:\n%s", view)
	}
}

func hasField(fields []string, want string) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
}

func fieldIndex(t *testing.T, fields []string, want string) int {
	t.Helper()
	for index, field := range fields {
		if field == want {
			return index
		}
	}
	t.Fatalf("field %q not found in %#v", want, fields)
	return 0
}
