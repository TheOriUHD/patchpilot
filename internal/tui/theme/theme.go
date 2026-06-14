package theme

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Styles contains reusable Lip Gloss styles for the TUI.
type Styles struct {
	Accent       lipgloss.TerminalColor
	Good         lipgloss.TerminalColor
	Warning      lipgloss.TerminalColor
	Bad          lipgloss.TerminalColor
	Muted        lipgloss.TerminalColor
	Base         lipgloss.Style
	Tab          lipgloss.Style
	ActiveTab    lipgloss.Style
	StatusBar    lipgloss.Style
	StatusOK     lipgloss.Style
	StatusBad    lipgloss.Style
	Card         lipgloss.Style
	CardTitle    lipgloss.Style
	MutedText    lipgloss.Style
	Button       lipgloss.Style
	ActiveButton lipgloss.Style
	ErrorText    lipgloss.Style
}

// NewStyles builds the TUI style palette and honors NO_COLOR.
func NewStyles() Styles {
	noColor := os.Getenv("NO_COLOR") != ""
	if noColor {
		return monochromeStyles()
	}

	accent := lipgloss.AdaptiveColor{Light: "#007C89", Dark: "#80E8FF"}
	good := lipgloss.AdaptiveColor{Light: "#1B7F4B", Dark: "#7EE2A8"}
	warning := lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#F4C76B"}
	bad := lipgloss.AdaptiveColor{Light: "#B42318", Dark: "#FF8A80"}
	muted := lipgloss.AdaptiveColor{Light: "#667085", Dark: "#98A2B3"}

	return Styles{
		Accent:  accent,
		Good:    good,
		Warning: warning,
		Bad:     bad,
		Muted:   muted,
		Base:    lipgloss.NewStyle(),
		Tab: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(muted),
		ActiveTab: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(accent).
			Bold(true),
		StatusBar: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(muted),
		StatusOK:  lipgloss.NewStyle().Foreground(good),
		StatusBad: lipgloss.NewStyle().Foreground(bad),
		Card: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(1, 2),
		CardTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(accent),
		MutedText: lipgloss.NewStyle().Foreground(muted),
		Button: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(muted).
			Padding(0, 2),
		ActiveButton: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Foreground(accent).
			Padding(0, 2),
		ErrorText: lipgloss.NewStyle().Foreground(bad),
	}
}

func monochromeStyles() Styles {
	neutral := lipgloss.Color("")
	return Styles{
		Accent:       neutral,
		Good:         neutral,
		Warning:      neutral,
		Bad:          neutral,
		Muted:        neutral,
		Base:         lipgloss.NewStyle(),
		Tab:          lipgloss.NewStyle().Padding(0, 1),
		ActiveTab:    lipgloss.NewStyle().Padding(0, 1).Underline(true),
		StatusBar:    lipgloss.NewStyle().Padding(0, 1),
		StatusOK:     lipgloss.NewStyle(),
		StatusBad:    lipgloss.NewStyle(),
		Card:         lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2),
		CardTitle:    lipgloss.NewStyle().Bold(true),
		MutedText:    lipgloss.NewStyle(),
		Button:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2),
		ActiveButton: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2).Underline(true),
		ErrorText:    lipgloss.NewStyle(),
	}
}
