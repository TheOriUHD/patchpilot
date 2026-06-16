package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/scheduler"
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
)

const doubleClickWindow = 400 * time.Millisecond

type regionAction string

const (
	regionActionTab         regionAction = "tab"
	regionActionHostRow     regionAction = "host-row"
	regionActionScheduleRow regionAction = "schedule-row"
	regionActionRunRow      regionAction = "run-row"
	regionActionButton      regionAction = "button"
	regionActionMenuItem    regionAction = "menu-item"
)

type region struct {
	ID     string
	Rect   Rect
	Action regionAction
	Index  int
	Name   string
}

type contextMenuState struct {
	Open        bool
	Kind        string
	X           int
	Y           int
	TargetID    int64
	TargetIndex int
	Items       []contextMenuItem
}

type contextMenuItem struct {
	Label    string
	Action   string
	Disabled bool
}

type regionRegistry struct {
	regions []region
}

func newRegionRegistry() *regionRegistry {
	return &regionRegistry{regions: []region{}}
}

func (r *regionRegistry) Reset() {
	if r == nil {
		return
	}
	r.regions = r.regions[:0]
}

func (r *regionRegistry) Add(item region) {
	if r == nil || item.Rect.Empty() {
		return
	}
	r.regions = append(r.regions, item)
}

func (r *regionRegistry) Hit(x int, y int) (region, bool) {
	if r == nil {
		return region{}, false
	}
	for index := len(r.regions) - 1; index >= 0; index-- {
		item := r.regions[index]
		if item.Rect.Contains(x, y) {
			return item, true
		}
	}
	return region{}, false
}

func (r *regionRegistry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.regions)
}

type lastMouseClick struct {
	X      int
	Y      int
	Button tea.MouseButton
	At     time.Time
	Valid  bool
}

func (m model) renderTabBar() string {
	rendered := make([]string, 0, len(m.tabs))
	x := 0
	for index, label := range m.tabs {
		button := components.Button{Label: label, Active: index == m.activeTab, Hover: index == m.hoveredTab}
		view := components.RenderTabButton(m.styles, button)
		rendered = append(rendered, view)
		if m.regions != nil {
			m.regions.Add(region{ID: tabRegionID(index), Rect: Rect{X: x, Y: 0, Width: lipgloss.Width(view), Height: 1}, Action: regionActionTab, Index: index})
		}
		x += lipgloss.Width(view)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...) + "\n"
}

func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Context-menu visibility transitions return clearScreenOnTransition (tea.ClearScreen).
	event := tea.MouseEvent(msg)
	m.lastMouseRegionID = ""
	m.lastMouseDoubleClick = false
	m.lastMouseButton = event.Button

	hit, ok := m.regions.Hit(event.X, event.Y)
	if event.Action == tea.MouseActionMotion {
		m = m.updateHover(hit, ok)
		return m, nil
	}

	if event.Action != tea.MouseActionPress {
		return m, nil
	}
	if m.contextMenu.Open && (!ok || hit.Action != regionActionMenuItem) {
		m.contextMenu = contextMenuState{}
		if ok {
			return m, clearScreenOnTransition()
		}
		if !ok {
			m.lastClick.Valid = false
			m = m.clearHover()
			return m, clearScreenOnTransition()
		}
	}
	if !ok {
		m.lastClick.Valid = false
		m = m.clearHover()
		return m, nil
	}

	now := time.Now()
	if m.mouseNow != nil {
		now = m.mouseNow()
	}
	doubleClick := m.lastClick.Valid && m.lastClick.X == event.X && m.lastClick.Y == event.Y && m.lastClick.Button == event.Button && now.Sub(m.lastClick.At) <= doubleClickWindow
	m.lastMouseRegionID = hit.ID
	m.lastMouseDoubleClick = doubleClick
	m = m.updateHover(hit, true)

	if doubleClick {
		m.lastClick = lastMouseClick{}
	} else {
		m.lastClick = lastMouseClick{X: event.X, Y: event.Y, Button: event.Button, At: now, Valid: true}
	}

	if event.Button == tea.MouseButtonRight {
		return m.openContextMenuForRegion(hit, event.X, event.Y)
	}
	if event.Button == tea.MouseButtonLeft && hit.Action == regionActionMenuItem {
		return m.activateContextMenuItem(hit.Index)
	}
	if event.Button == tea.MouseButtonLeft && hit.Action == regionActionTab {
		m.activeTab = hit.Index
	}
	if event.Button == tea.MouseButtonLeft {
		switch hit.Action {
		case regionActionHostRow:
			m.hostSelected = hit.Index
			if doubleClick {
				m.hostScreen = hostScreenDetail
				if host, ok := m.selectedHost(); ok {
					m.renameBuffer = host.Name
				}
				return m, clearScreenOnTransition()
			}
		case regionActionScheduleRow:
			m.scheduleSelected = hit.Index
			if doubleClick {
				if schedule, ok := m.selectedSchedule(); ok {
					m.scheduleScreen = scheduleScreenEditor
					m.scheduleEditor = scheduleEditor{id: schedule.ID, input: scheduler.InputFromSchedule(schedule)}
					m.status = "Edit schedule"
					return m, clearScreenOnTransition()
				}
			}
		case regionActionRunRow:
			m.runSelected = hit.Index
			if doubleClick {
				if run, ok := m.selectedRun(); ok {
					return m, loadRunDetailCmd(m.ctx, m.client, run.ID)
				}
			}
		case regionActionButton:
			return m.activateButton(hit.Name)
		}
	}
	return m, nil
}

func (m model) updateHover(hit region, ok bool) model {
	m = m.clearHover()
	if !ok {
		return m
	}
	m.hoveredRegionID = hit.ID
	switch hit.Action {
	case regionActionTab:
		m.hoveredTab = hit.Index
	case regionActionHostRow:
		m.hoveredHost = hit.Index
		m.hostSelected = hit.Index
	case regionActionScheduleRow:
		m.hoveredSchedule = hit.Index
		m.scheduleSelected = hit.Index
	case regionActionRunRow:
		m.hoveredRun = hit.Index
		m.runSelected = hit.Index
	}
	return m
}

func (m model) clearHover() model {
	m.hoveredRegionID = ""
	m.hoveredTab = -1
	m.hoveredHost = -1
	m.hoveredSchedule = -1
	m.hoveredRun = -1
	return m
}

func (m model) openContextMenuForRegion(hit region, x int, y int) (tea.Model, tea.Cmd) {
	switch hit.Action {
	case regionActionHostRow:
		return m.openHostContextMenu(hit.Index, x, y), clearScreenOnTransition()
	case regionActionScheduleRow:
		return m.openScheduleContextMenu(hit.Index, x, y), clearScreenOnTransition()
	case regionActionRunRow:
		return m.openRunContextMenu(hit.Index, x, y), clearScreenOnTransition()
	default:
		return m, nil
	}
}

func (m model) activateContextMenuItem(index int) (tea.Model, tea.Cmd) {
	if !m.contextMenu.Open || index < 0 || index >= len(m.contextMenu.Items) {
		return m, nil
	}
	item := m.contextMenu.Items[index]
	if item.Disabled {
		return m, nil
	}
	m.contextMenu = contextMenuState{}
	updated, cmd := m.activateContextAction(item.Action)
	return updated, withClearScreen(cmd)
}

func (m model) renderContextMenuOverlay(body string, bodyHeight int) string {
	if !m.contextMenu.Open {
		return body
	}
	menu := components.ContextMenu{Title: m.contextMenu.Kind, Selected: -1}
	for _, item := range m.contextMenu.Items {
		menu.Items = append(menu.Items, components.MenuItem{Label: item.Label, Disabled: item.Disabled})
	}
	rendered := components.RenderContextMenu(m.styles, menu)
	menuLines := strings.Split(rendered, "\n")
	menuY := max(0, min(bodyHeight-1, m.contextMenu.Y-m.renderBodyY))
	menuX := max(0, min(max(0, m.width-1), m.contextMenu.X))
	for index, item := range m.contextMenu.Items {
		m.regions.Add(region{ID: fmt.Sprintf("menu:%d", index), Rect: Rect{X: menuX, Y: m.renderBodyY + menuY + 2 + index, Width: lipgloss.Width(menuLines[min(len(menuLines)-1, 2+index)]), Height: 1}, Action: regionActionMenuItem, Index: index, Name: item.Action})
	}
	return overlayLines(body, rendered, menuX, menuY, m.width, bodyHeight)
}

func overlayLines(base string, overlay string, x int, y int, width int, height int) string {
	lines := strings.Split(strings.TrimSuffix(base, "\n"), "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	overlayLines := strings.Split(strings.TrimSuffix(overlay, "\n"), "\n")
	for row, line := range overlayLines {
		target := y + row
		if target < 0 || target >= height {
			continue
		}
		baseLine := padRight(lines[target], width)
		prefix := baseLine[:min(x, len(baseLine))]
		lineWidth := lipgloss.Width(line)
		suffixStart := min(width, x+lineWidth)
		suffix := ""
		if suffixStart < len(baseLine) {
			suffix = baseLine[suffixStart:]
		}
		lines[target] = prefix + line + suffix
	}
	return strings.Join(lines[:height], "\n")
}

func padRight(value string, width int) string {
	current := lipgloss.Width(value)
	if current >= width {
		return value
	}
	return value + strings.Repeat(" ", width-current)
}

func tabRegionID(index int) string {
	return fmt.Sprintf("tab:%d", index)
}

func (m model) openHostContextMenu(index int, x int, y int) model {
	hosts := m.filteredHosts()
	if index < 0 || index >= len(hosts) {
		return m
	}
	m.hostSelected = index
	host := hosts[index]
	m.contextMenu = contextMenuState{Open: true, Kind: "Host " + host.Name, X: x, Y: y, TargetID: host.ID, TargetIndex: index, Items: []contextMenuItem{
		{Label: "Update now", Action: "host:update"},
		{Label: "Dry run", Action: "host:dry-run"},
		{Label: "Edit settings", Action: "host:settings"},
		{Label: "Reboot policy", Action: "host:reboot-policy"},
		{Label: "Rotate sudoers", Action: "host:repair-sudoers"},
		{Label: "Remove", Action: "host:remove"},
	}}
	return m
}

func (m model) openScheduleContextMenu(index int, x int, y int) model {
	if index < 0 || index >= len(m.schedules) {
		return m
	}
	m.scheduleSelected = index
	schedule := m.schedules[index]
	m.contextMenu = contextMenuState{Open: true, Kind: "Schedule " + schedule.Name, X: x, Y: y, TargetID: schedule.ID, TargetIndex: index, Items: []contextMenuItem{
		{Label: "Run now", Action: "schedule:run"},
		{Label: "Edit", Action: "schedule:edit"},
		{Label: "Delete", Action: "schedule:delete"},
	}}
	return m
}

func (m model) openRunContextMenu(index int, x int, y int) model {
	if index < 0 || index >= len(m.runs) {
		return m
	}
	m.runSelected = index
	run := m.runs[index]
	m.contextMenu = contextMenuState{Open: true, Kind: fmt.Sprintf("Run %d", run.ID), X: x, Y: y, TargetID: run.ID, TargetIndex: index, Items: []contextMenuItem{
		{Label: "Open log", Action: "run:open"},
		{Label: "Export (coming soon)", Action: "run:export"},
	}}
	return m
}

func (m model) activateContextAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "host:update":
		if host, ok := m.selectedHost(); ok {
			m.pending = pendingAction{Kind: "run", Host: host}
		}
	case "host:dry-run":
		if host, ok := m.selectedHost(); ok {
			m.status = "Dry run started"
			return m, dryRunHostCmd(m.ctx, m.client, host.ID)
		}
	case "host:settings":
		if host, ok := m.selectedHost(); ok {
			m.hostScreen = hostScreenDetail
			m.renameBuffer = host.Name
			return m, nil
		}
	case "host:reboot-policy":
		if host, ok := m.selectedHost(); ok {
			input := hostUpdateFromHost(host)
			input.AutoReboot = !host.AutoReboot
			input.AutoRebootDelay = core.NormalizeRebootDelay(host.AutoRebootDelay)
			message := "Reboot policy set to report only"
			if input.AutoReboot {
				message = "Reboot policy set to auto-reboot"
			}
			return m, updateHostCmd(m.ctx, m.client, host.ID, input, message)
		}
	case "host:repair-sudoers":
		if host, ok := m.selectedHost(); ok {
			m.pending = pendingAction{Kind: "repair-sudoers", Host: host}
		}
	case "host:remove":
		if host, ok := m.selectedHost(); ok {
			m.pending = pendingAction{Kind: "remove", Host: host}
		}
	case "schedule:run":
		if schedule, ok := m.selectedSchedule(); ok {
			m.schedulePending = schedulePendingAction{Kind: "run", ID: schedule.ID, Name: schedule.Name}
		}
	case "schedule:edit":
		if schedule, ok := m.selectedSchedule(); ok {
			m.scheduleScreen = scheduleScreenEditor
			m.scheduleEditor = scheduleEditor{id: schedule.ID, input: scheduler.InputFromSchedule(schedule)}
			m.status = "Edit schedule"
		}
	case "schedule:delete":
		if schedule, ok := m.selectedSchedule(); ok {
			m.schedulePending = schedulePendingAction{Kind: "delete", ID: schedule.ID, Name: schedule.Name}
		}
	case "run:open":
		if run, ok := m.selectedRun(); ok {
			return m, loadRunDetailCmd(m.ctx, m.client, run.ID)
		}
	case "run:export":
		m.status = "Export coming soon"
	}
	return m, nil
}

func (m model) activateButton(name string) (tea.Model, tea.Cmd) {
	switch name {
	case "host:add":
		m.hostScreen = hostScreenWizard
		m.wizard = newStandardWizard()
		m.status = "Add host"
	case "host:run-selected":
		if host, ok := m.selectedHost(); ok {
			m.pending = pendingAction{Kind: "run", Host: host}
		}
	case "host:refresh":
		m.hostLoading = true
		return m, loadHostsCmd(m.ctx, m.client)
	}
	return m, nil
}
