package tui

import (
	"testing"

	"github.com/TheOriUHD/patchpilot/internal/core"
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

func hasField(fields []string, want string) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
}
