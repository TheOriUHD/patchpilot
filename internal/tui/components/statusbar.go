package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

// DaemonState describes the daemon connection indicator.
type DaemonState int

const (
	// DaemonChecking means the TUI is waiting on the health check.
	DaemonChecking DaemonState = iota
	// DaemonUp means the daemon responded successfully.
	DaemonUp
	// DaemonDown means the daemon is unreachable.
	DaemonDown
)

// StatusBarState describes the bottom status bar.
type StatusBarState struct {
	Keybinds []string
	Daemon   DaemonState
	Message  string
	Width    int
}

// RenderStatusBar renders keybinds and daemon state for the bottom chrome.
func RenderStatusBar(styles theme.Styles, state StatusBarState) string {
	innerWidth := state.Width - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	left := strings.Join(state.Keybinds, "  ")
	if state.Message != "" {
		left = left + "  " + styles.MutedText.Render(state.Message)
	}

	indicator := daemonIndicator(styles, state.Daemon)
	gap := innerWidth - lipgloss.Width(left) - lipgloss.Width(indicator)
	if gap < 1 {
		gap = 1
	}

	line := fmt.Sprintf("%s%s%s", left, strings.Repeat(" ", gap), indicator)
	return styles.StatusBar.Width(innerWidth).Render(line)
}

func daemonIndicator(styles theme.Styles, state DaemonState) string {
	switch state {
	case DaemonUp:
		return styles.StatusOK.Render("● daemon ok")
	case DaemonDown:
		return styles.StatusBad.Render("● daemon down")
	default:
		return styles.MutedText.Render("● checking daemon")
	}
}
