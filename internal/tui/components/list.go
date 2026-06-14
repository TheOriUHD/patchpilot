package components

import "github.com/TheOriUHD/patchpilot/internal/tui/theme"

// Row describes a selectable list row.
type Row struct {
	Text     string
	Selected bool
	Hover    bool
	Disabled bool
}

// RenderListRow renders a row with keyboard selection and mouse hover states.
func RenderListRow(styles theme.Styles, row Row) string {
	switch {
	case row.Disabled:
		return styles.MutedText.Render(row.Text)
	case row.Selected:
		return styles.ActiveTab.Render(row.Text)
	case row.Hover:
		return styles.HoverRow.Render(row.Text)
	default:
		return row.Text
	}
}
