package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
)

const doubleClickWindow = 400 * time.Millisecond

type regionAction string

const (
	regionActionTab regionAction = "tab"
)

type region struct {
	ID     string
	Rect   Rect
	Action regionAction
	Index  int
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
	event := tea.MouseEvent(msg)
	m.lastMouseRegionID = ""
	m.lastMouseDoubleClick = false
	m.lastMouseButton = event.Button

	hit, ok := m.regions.Hit(event.X, event.Y)
	if event.Action == tea.MouseActionMotion {
		if ok && hit.Action == regionActionTab {
			m.hoveredRegionID = hit.ID
			m.hoveredTab = hit.Index
		} else {
			m.hoveredRegionID = ""
			m.hoveredTab = -1
		}
		return m, nil
	}

	if event.Action != tea.MouseActionPress {
		return m, nil
	}
	if !ok {
		m.lastClick.Valid = false
		m.hoveredRegionID = ""
		m.hoveredTab = -1
		return m, nil
	}

	now := time.Now()
	if m.mouseNow != nil {
		now = m.mouseNow()
	}
	doubleClick := m.lastClick.Valid && m.lastClick.X == event.X && m.lastClick.Y == event.Y && m.lastClick.Button == event.Button && now.Sub(m.lastClick.At) <= doubleClickWindow
	m.lastMouseRegionID = hit.ID
	m.lastMouseDoubleClick = doubleClick
	m.hoveredRegionID = hit.ID
	if hit.Action == regionActionTab {
		m.hoveredTab = hit.Index
	}

	if doubleClick {
		m.lastClick = lastMouseClick{}
	} else {
		m.lastClick = lastMouseClick{X: event.X, Y: event.Y, Button: event.Button, At: now, Valid: true}
	}

	if event.Button == tea.MouseButtonLeft && hit.Action == regionActionTab {
		m.activeTab = hit.Index
	}
	return m, nil
}

func tabRegionID(index int) string {
	return fmt.Sprintf("tab:%d", index)
}
