package components

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

// Button describes a visible clickable command or tab.
type Button struct {
	Label    string
	Active   bool
	Hover    bool
	Disabled bool
}

// RenderTabButton renders a tab as a button-like element.
func RenderTabButton(styles theme.Styles, button Button) string {
	return button.Render(styles.Tab, styles.ActiveTab, styles.HoverTab, styles.MutedText)
}

// Render renders the button using the supplied state styles.
func (b Button) Render(normal lipgloss.Style, active lipgloss.Style, hover lipgloss.Style, disabled lipgloss.Style) string {
	text := "[ " + b.Label + " ]"
	switch {
	case b.Disabled:
		return disabled.Render(text)
	case b.Active:
		return active.Render(text)
	case b.Hover:
		return hover.Render(text)
	default:
		return normal.Render(text)
	}
}
