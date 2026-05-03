package components

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

// NewSpinner returns the themed spinner used for genuine waits.
func NewSpinner(styles theme.Styles) spinner.Model {
	model := spinner.New()
	model.Spinner = spinner.MiniDot
	model.Style = model.Style.Foreground(styles.Accent)
	return model
}
