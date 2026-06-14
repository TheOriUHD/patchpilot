package components

import (
	"strings"
	"testing"

	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

func TestRenderTabBarSingleLine(t *testing.T) {
	styles := theme.NewStyles()
	labels := []string{"Hosts", "Schedules", "Logs", "Settings"}

	for active := range labels {
		rendered := RenderTabBar(styles, TabBarState{Labels: labels, Active: active})
		t.Logf("active %d: %s", active, strings.TrimSuffix(rendered, "\n"))
		if got := strings.Count(rendered, "\n"); got != 1 {
			t.Fatalf("active tab %d rendered with %d newlines, want 1: %q", active, got, rendered)
		}
		if !strings.HasSuffix(rendered, "\n") {
			t.Fatalf("active tab %d rendered without trailing newline: %q", active, rendered)
		}
	}
}
