package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
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

func TestHostRowClickSelectsAndDoubleClickOpensDetail(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	m := mouseTestModel()
	m.mouseNow = func() time.Time { return now }
	m.hosts = []core.Host{{ID: 1, Name: "alpha", Type: core.HostTypeStandard, Enabled: true}, {ID: 2, Name: "bravo", Type: core.HostTypeStandard, Enabled: true}}
	_ = m.View()
	x, y := regionCenter(t, m, regionActionHostRow, 1, "")
	updated, _ := m.handleMouse(mousePress(x, y, tea.MouseButtonLeft))
	m = updated.(model)
	if m.hostSelected != 1 {
		t.Fatalf("hostSelected = %d, want 1", m.hostSelected)
	}
	now = now.Add(200 * time.Millisecond)
	updated, _ = m.handleMouse(mousePress(x, y, tea.MouseButtonLeft))
	m = updated.(model)
	if m.hostScreen != hostScreenDetail {
		t.Fatalf("hostScreen = %d, want detail", m.hostScreen)
	}
}

func TestScheduleRowClickAndDoubleClick(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	m := mouseTestModel()
	m.activeTab = 1
	m.mouseNow = func() time.Time { return now }
	m.schedules = []core.Schedule{{ID: 1, Name: "daily", Frequency: core.FrequencyDaily}, {ID: 2, Name: "weekly", Frequency: core.FrequencyWeekly}}
	_ = m.View()
	x, y := regionCenter(t, m, regionActionScheduleRow, 1, "")
	updated, _ := m.handleMouse(mousePress(x, y, tea.MouseButtonLeft))
	m = updated.(model)
	if m.scheduleSelected != 1 {
		t.Fatalf("scheduleSelected = %d, want 1", m.scheduleSelected)
	}
	now = now.Add(200 * time.Millisecond)
	updated, _ = m.handleMouse(mousePress(x, y, tea.MouseButtonLeft))
	m = updated.(model)
	if m.scheduleScreen != scheduleScreenEditor || m.scheduleEditor.id != 2 {
		t.Fatalf("schedule editor state = screen %d id %d, want editor id 2", m.scheduleScreen, m.scheduleEditor.id)
	}
}

func TestRunRowClickAndDoubleClickDispatchesOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	m := mouseTestModel()
	m.activeTab = 2
	m.mouseNow = func() time.Time { return now }
	m.runs = []core.Run{{ID: 1, Trigger: core.RunTriggerManual, Status: core.RunStatusSuccess, StartedAt: now}, {ID: 2, Trigger: core.RunTriggerManual, Status: core.RunStatusSuccess, StartedAt: now}}
	_ = m.View()
	x, y := regionCenter(t, m, regionActionRunRow, 1, "")
	updated, cmd := m.handleMouse(mousePress(x, y, tea.MouseButtonLeft))
	m = updated.(model)
	if cmd != nil {
		t.Fatal("single-click run row should not dispatch open command")
	}
	if m.runSelected != 1 {
		t.Fatalf("runSelected = %d, want 1", m.runSelected)
	}
	now = now.Add(200 * time.Millisecond)
	updated, cmd = m.handleMouse(mousePress(x, y, tea.MouseButtonLeft))
	m = updated.(model)
	if cmd == nil {
		t.Fatal("double-click run row should dispatch open command")
	}
}

func TestRightClickAndMOpenContextMenu(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	m.hosts = []core.Host{{ID: 1, Name: "alpha", Type: core.HostTypeStandard, Enabled: true}}
	_ = m.View()
	x, y := regionCenter(t, m, regionActionHostRow, 0, "")
	updated, _ := m.handleMouse(mousePress(x, y, tea.MouseButtonRight))
	m = updated.(model)
	if !m.contextMenu.Open || m.contextMenu.Kind != "Host alpha" {
		t.Fatalf("context menu = %#v, want host menu", m.contextMenu)
	}
	m.contextMenu = contextMenuState{}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = updated.(model)
	if !m.contextMenu.Open || m.contextMenu.Kind != "Host alpha" {
		t.Fatalf("m key context menu = %#v, want host menu", m.contextMenu)
	}
}

func TestContextMenuClosesOnOutsideClickAndEsc(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	m.hosts = []core.Host{{ID: 1, Name: "alpha", Type: core.HostTypeStandard, Enabled: true}}
	_ = m.View()
	x, y := regionCenter(t, m, regionActionHostRow, 0, "")
	updated, _ := m.handleMouse(mousePress(x, y, tea.MouseButtonRight))
	m = updated.(model)
	updated, _ = m.handleMouse(mousePress(119, 39, tea.MouseButtonLeft))
	m = updated.(model)
	if m.contextMenu.Open {
		t.Fatal("context menu stayed open after outside click")
	}
	updated, _ = m.handleMouse(mousePress(x, y, tea.MouseButtonRight))
	m = updated.(model)
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.contextMenu.Open {
		t.Fatal("context menu stayed open after Esc")
	}
}

func TestContextMenuItemDispatchesAction(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	m.hosts = []core.Host{{ID: 1, Name: "alpha", Type: core.HostTypeStandard, Enabled: true}}
	_ = m.View()
	x, y := regionCenter(t, m, regionActionHostRow, 0, "")
	updated, _ := m.handleMouse(mousePress(x, y, tea.MouseButtonRight))
	m = updated.(model)
	_ = m.View()
	menuX, menuY := regionCenter(t, m, regionActionMenuItem, 0, "")
	updated, cmd := m.handleMouse(mousePress(menuX, menuY, tea.MouseButtonLeft))
	m = updated.(model)
	if cmd == nil {
		t.Fatal("Update now menu action should return clear-screen command")
	}
	if m.contextMenu.Open {
		t.Fatal("context menu stayed open after item selection")
	}
	if m.pending.Kind != "run" {
		t.Fatalf("pending kind = %q, want run", m.pending.Kind)
	}
}

func TestContextMenuItemRegionsMatchPaintedItems(t *testing.T) {
	t.Parallel()

	items := []contextMenuItem{
		{Label: "Update now", Action: "test:update"},
		{Label: "Dry run", Action: "test:dry-run"},
		{Label: "Edit settings", Action: "test:settings"},
		{Label: "Remove", Action: "test:remove"},
	}
	tests := []struct {
		name        string
		title       string
		x           int
		y           int
		wantShiftUp bool
	}{
		{name: "title present", title: "Host alpha", x: 12, y: 8},
		{name: "title absent", title: "", x: 12, y: 8},
		{name: "bottom clamped", title: "Host alpha", x: 118, y: 38, wantShiftUp: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for index := range items {
				m := mouseTestModel()
				m.contextMenu = contextMenuState{Open: true, Kind: test.title, X: test.x, Y: test.y, Items: items}
				_ = m.View()

				x, y, shiftedUp := paintedContextMenuItemCenter(t, m, index)
				if test.wantShiftUp && !shiftedUp {
					t.Fatal("context menu was not shifted up from bottom edge")
				}
				updated, _ := m.handleMouse(mousePress(x, y, tea.MouseButtonLeft))
				m = updated.(model)
				if want := fmt.Sprintf("menu:%d", index); m.lastMouseRegionID != want {
					t.Fatalf("click item %d at %d,%d hit %q, want %q", index, x, y, m.lastMouseRegionID, want)
				}
			}
		})
	}
}

func TestHostActionButtonsAndKeyboardAccelerators(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	m.hosts = []core.Host{{ID: 1, Name: "alpha", Type: core.HostTypeStandard, Enabled: true}}
	_ = m.View()
	x, y := regionCenter(t, m, regionActionButton, 0, "host:add")
	updated, _ := m.handleMouse(mousePress(x, y, tea.MouseButtonLeft))
	clicked := updated.(model)
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	keyed := updated.(model)
	if clicked.hostScreen != hostScreenWizard || keyed.hostScreen != hostScreenWizard {
		t.Fatalf("add button/key screens = %d/%d, want wizard", clicked.hostScreen, keyed.hostScreen)
	}

	m = mouseTestModel()
	m.hosts = []core.Host{{ID: 1, Name: "alpha", Type: core.HostTypeStandard, Enabled: true}}
	_ = m.View()
	x, y = regionCenter(t, m, regionActionButton, 0, "host:run-selected")
	updated, _ = m.handleMouse(mousePress(x, y, tea.MouseButtonLeft))
	clicked = updated.(model)
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	keyed = updated.(model)
	if clicked.pending.Kind != "run" || keyed.pending.Kind != "run" {
		t.Fatalf("run button/key pending = %q/%q, want run", clicked.pending.Kind, keyed.pending.Kind)
	}
}

func TestHoverThenArrowShowsOneActiveRow(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	m.hosts = []core.Host{{ID: 1, Name: "alpha", Type: core.HostTypeStandard, Enabled: true}, {ID: 2, Name: "bravo", Type: core.HostTypeStandard, Enabled: true}, {ID: 3, Name: "charlie", Type: core.HostTypeStandard, Enabled: true}}
	_ = m.View()
	x, y := regionCenter(t, m, regionActionHostRow, 1, "")
	updated, _ := m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	m = updated.(model)
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.hostSelected != 2 || m.hoveredHost != -1 {
		t.Fatalf("hostSelected=%d hoveredHost=%d, want selected row 3 and no hover", m.hostSelected, m.hoveredHost)
	}
	view := m.View()
	if count := strings.Count(view, theme.SymbolPointer); count != 1 {
		t.Fatalf("active row count = %d, want 1", count)
	}
}

func TestArrowThenHoverShowsOneActiveRow(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	m.hosts = []core.Host{{ID: 1, Name: "alpha", Type: core.HostTypeStandard, Enabled: true}, {ID: 2, Name: "bravo", Type: core.HostTypeStandard, Enabled: true}, {ID: 3, Name: "charlie", Type: core.HostTypeStandard, Enabled: true}, {ID: 4, Name: "delta", Type: core.HostTypeStandard, Enabled: true}}
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	_ = m.View()
	x, y := regionCenter(t, m, regionActionHostRow, 3, "")
	updated, _ = m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	m = updated.(model)
	if m.hostSelected != 3 || m.hoveredHost != 3 {
		t.Fatalf("hostSelected=%d hoveredHost=%d, want row 4 active via hover", m.hostSelected, m.hoveredHost)
	}
	view := m.View()
	if count := strings.Count(view, theme.SymbolPointer); count != 1 {
		t.Fatalf("active row count = %d, want 1", count)
	}
}

func TestSyntheticMouseMotionHoverRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		prepare      func(model) model
		action       regionAction
		index        int
		wantHost     int
		wantSchedule int
		wantRun      int
	}{
		{
			name: "hosts",
			prepare: func(m model) model {
				m.hosts = []core.Host{{ID: 1, Name: "alpha", Type: core.HostTypeStandard, Enabled: true}, {ID: 2, Name: "bravo", Type: core.HostTypeStandard, Enabled: true}}
				return m
			},
			action: regionActionHostRow, index: 1, wantHost: 1, wantSchedule: -1, wantRun: -1,
		},
		{
			name: "schedules",
			prepare: func(m model) model {
				m.activeTab = 1
				m.schedules = []core.Schedule{{ID: 1, Name: "daily", Frequency: core.FrequencyDaily}, {ID: 2, Name: "weekly", Frequency: core.FrequencyWeekly}}
				return m
			},
			action: regionActionScheduleRow, index: 1, wantHost: -1, wantSchedule: 1, wantRun: -1,
		},
		{
			name: "runs",
			prepare: func(m model) model {
				m.activeTab = 2
				m.runs = []core.Run{{ID: 1, Trigger: core.RunTriggerManual, Status: core.RunStatusSuccess, StartedAt: time.Now()}, {ID: 2, Trigger: core.RunTriggerManual, Status: core.RunStatusSuccess, StartedAt: time.Now()}}
				return m
			},
			action: regionActionRunRow, index: 1, wantHost: -1, wantSchedule: -1, wantRun: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := test.prepare(mouseTestModel())
			_ = m.View()
			x, y := regionCenter(t, m, test.action, test.index, "")
			updated, _ := m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
			m = updated.(model)
			if m.hoveredHost != test.wantHost || m.hoveredSchedule != test.wantSchedule || m.hoveredRun != test.wantRun {
				t.Fatalf("hover state host=%d schedule=%d run=%d, want %d/%d/%d", m.hoveredHost, m.hoveredSchedule, m.hoveredRun, test.wantHost, test.wantSchedule, test.wantRun)
			}
		})
	}
}

func TestMouseClickUnregisteredAreaNoHit(t *testing.T) {
	t.Parallel()

	m := mouseTestModel()
	_ = m.View()
	updated, _ := m.handleMouse(mousePress(119, 39, tea.MouseButtonLeft))
	m = updated.(model)
	if m.lastMouseRegionID != "" || m.lastMouseDoubleClick {
		t.Fatalf("unregistered click hit region=%q double=%v", m.lastMouseRegionID, m.lastMouseDoubleClick)
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

func regionCenter(t *testing.T, model model, action regionAction, index int, name string) (int, int) {
	t.Helper()
	for _, region := range model.regions.regions {
		if region.Action != action {
			continue
		}
		if name != "" && region.Name != name {
			continue
		}
		if name == "" && region.Index != index {
			continue
		}
		return region.Rect.X + max(0, region.Rect.Width/2), region.Rect.Y
	}
	t.Fatalf("region action=%s index=%d name=%q not found in %#v", action, index, name, model.regions.regions)
	return 0, 0
}

func paintedContextMenuItemCenter(t *testing.T, model model, index int) (int, int, bool) {
	t.Helper()

	menu := components.ContextMenu{Title: model.contextMenu.Kind, Selected: -1}
	for _, item := range model.contextMenu.Items {
		menu.Items = append(menu.Items, components.MenuItem{Label: item.Label, Disabled: item.Disabled})
	}
	rendered, itemYOffsets, contentX, boxWidth := components.RenderContextMenuWithOffsets(model.styles, menu)
	if index < 0 || index >= len(itemYOffsets) {
		t.Fatalf("item offset %d missing from %d offsets", index, len(itemYOffsets))
	}
	bodyY := contextMenuBodyYForTest(model)
	bodyHeight := contextMenuBodyHeightForTest(model, bodyY)
	boxHeight := lipgloss.Height(rendered)
	preferredY := model.contextMenu.Y - bodyY
	menuY := max(0, min(max(0, bodyHeight-boxHeight), preferredY))
	menuX := max(0, min(max(0, model.width-boxWidth), model.contextMenu.X))
	if menuY+boxHeight > bodyHeight {
		t.Fatalf("menu bottom = %d, body height = %d", menuY+boxHeight, bodyHeight)
	}
	labelWidth := lipgloss.Width("  " + model.contextMenu.Items[index].Label)
	return menuX + contentX + max(0, labelWidth/2), bodyY + menuY + itemYOffsets[index], menuY < preferredY
}

func contextMenuBodyYForTest(model model) int {
	tabbar := strings.TrimSuffix(model.renderTabBar(), "\n")
	return lipgloss.Height(tabbar)
}

func contextMenuBodyHeightForTest(model model, bodyY int) int {
	status := components.RenderStatusBar(model.styles, components.StatusBarState{
		Keybinds: model.keybinds(),
		Daemon:   model.daemonState,
		Message:  model.status,
		Width:    model.width,
	})
	bodyHeight := model.height - bodyY - lipgloss.Height(status)
	if bodyHeight < 1 {
		return 1
	}
	return bodyHeight
}
