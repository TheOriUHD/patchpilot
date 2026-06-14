package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
)

func TestRegionRegistryHitAndReset(t *testing.T) {
	t.Parallel()

	registry := newRegionRegistry()
	registry.Add(region{ID: "first", Rect: Rect{X: 1, Y: 2, Width: 4, Height: 2}, Action: regionActionTab, Index: 0})
	if registry.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", registry.Len())
	}
	hit, ok := registry.Hit(2, 3)
	if !ok || hit.ID != "first" {
		t.Fatalf("Hit() = %#v, %v; want first hit", hit, ok)
	}
	if _, ok := registry.Hit(0, 0); ok {
		t.Fatal("Hit() outside region returned ok")
	}
	registry.Reset()
	if registry.Len() != 0 {
		t.Fatalf("Len() after reset = %d, want 0", registry.Len())
	}
}

func TestViewClearsRegionsPerFrame(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	_ = m.View()
	if got := m.regions.Len(); got != 4 {
		t.Fatalf("regions after first view = %d, want 4", got)
	}
	m.tabs = []string{"Only"}
	_ = m.View()
	if got := m.regions.Len(); got != 1 {
		t.Fatalf("regions after second view = %d, want 1", got)
	}
}

func TestMouseClickTabsSwitchesActiveTab(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	_ = m.View()
	for index := range m.tabs {
		region, ok := m.regions.Hit(tabCenterX(t, m, index), 0)
		if !ok || region.Index != index {
			t.Fatalf("tab %d region missing: %#v %v", index, region, ok)
		}
		updated, _ := m.handleMouse(mousePress(tabCenterX(t, m, index), 0, tea.MouseButtonLeft))
		m = updated.(model)
		if m.activeTab != index {
			t.Fatalf("activeTab after clicking tab %d = %d", index, m.activeTab)
		}
	}
}

func TestMouseHoverTabVisibleInRender(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	_ = m.View()
	updated, _ := m.handleMouse(tea.MouseMsg{X: tabCenterX(t, m, 1), Y: 0, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	m = updated.(model)
	if m.hoveredTab != 1 {
		t.Fatalf("hoveredTab = %d, want 1", m.hoveredTab)
	}
	view := m.renderTabBar()
	if !strings.Contains(view, m.styles.HoverTab.Render("[ Schedules ]")) {
		t.Fatalf("hovered tab render missing hover style: %q", view)
	}
}

func TestDoubleClickDetection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	m := mouseTestModel()
	m.mouseNow = func() time.Time { return now }
	_ = m.View()

	updated, _ := m.handleMouse(mousePress(tabCenterX(t, m, 1), 0, tea.MouseButtonLeft))
	m = updated.(model)
	if m.lastMouseDoubleClick {
		t.Fatal("first click was marked double-click")
	}
	now = now.Add(250 * time.Millisecond)
	updated, _ = m.handleMouse(mousePress(tabCenterX(t, m, 1), 0, tea.MouseButtonLeft))
	m = updated.(model)
	if !m.lastMouseDoubleClick {
		t.Fatal("second click within double-click window was not marked double-click")
	}
}

func TestDoubleClickNegativeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		secondX      int
		secondButton tea.MouseButton
		delay        time.Duration
	}{
		{name: "too slow", secondX: 0, secondButton: tea.MouseButtonLeft, delay: 401 * time.Millisecond},
		{name: "different position", secondX: 1, secondButton: tea.MouseButtonLeft, delay: 200 * time.Millisecond},
		{name: "different button", secondX: 0, secondButton: tea.MouseButtonRight, delay: 200 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
			m := mouseTestModel()
			m.mouseNow = func() time.Time { return now }
			_ = m.View()
			firstX := tabCenterX(t, m, 0)
			updated, _ := m.handleMouse(mousePress(firstX, 0, tea.MouseButtonLeft))
			m = updated.(model)
			now = now.Add(test.delay)
			updated, _ = m.handleMouse(mousePress(firstX+test.secondX, 0, test.secondButton))
			m = updated.(model)
			if m.lastMouseDoubleClick {
				t.Fatal("negative case was marked double-click")
			}
		})
	}
}

func mouseTestModel() model {
	model := newModel(context.Background(), "/tmp/patchpilot.sock")
	model.width = 120
	model.height = 40
	model.daemonState = components.DaemonUp
	return model
}

func mousePress(x int, y int, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button}
}

func tabCenterX(t *testing.T, model model, index int) int {
	t.Helper()
	for _, region := range model.regions.regions {
		if region.Action == regionActionTab && region.Index == index {
			return region.Rect.X + max(0, region.Rect.Width/2)
		}
	}
	t.Fatalf("tab region %d not found", index)
	return 0
}
