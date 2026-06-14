package components

import (
	"strings"

	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

// MenuItem describes one context-menu action.
type MenuItem struct {
	Label    string
	Disabled bool
}

// ContextMenu is a simple vertical menu rendered at a hit-tested position.
type ContextMenu struct {
	Title    string
	Items    []MenuItem
	Selected int
}

// RenderContextMenu renders a menu for future mouse/right-click workflows.
func RenderContextMenu(styles theme.Styles, menu ContextMenu) string {
	var builder strings.Builder
	if menu.Title != "" {
		builder.WriteString(styles.CardTitle.Render(menu.Title))
		builder.WriteByte('\n')
	}
	for index, item := range menu.Items {
		prefix := "  "
		if index == menu.Selected {
			prefix = theme.SymbolPointer + " "
		}
		line := prefix + item.Label
		if item.Disabled {
			builder.WriteString(styles.MutedText.Render(line))
		} else if index == menu.Selected {
			builder.WriteString(styles.ActiveTab.Render(line))
		} else {
			builder.WriteString(line)
		}
		if index < len(menu.Items)-1 {
			builder.WriteByte('\n')
		}
	}
	return styles.Card.Render(builder.String())
}
