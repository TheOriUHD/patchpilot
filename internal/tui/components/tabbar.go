package components

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

// TabBarState describes the top-level tabs to render.
type TabBarState struct {
	Labels []string
	Active int
	Hover  int
}

// RenderTabBar renders the global tab bar.
func RenderTabBar(styles theme.Styles, state TabBarState) string {
	rendered := make([]string, 0, len(state.Labels))
	for index, label := range state.Labels {
		rendered = append(rendered, RenderTabButton(styles, Button{Label: label, Active: index == state.Active, Hover: index == state.Hover}))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...) + "\n"
}
