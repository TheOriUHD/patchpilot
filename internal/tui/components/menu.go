package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	rendered, _, _, _ := RenderContextMenuWithOffsets(styles, menu)
	return rendered
}

// RenderContextMenuWithOffsets renders a menu and returns item geometry relative to the box top-left.
func RenderContextMenuWithOffsets(styles theme.Styles, menu ContextMenu) (rendered string, itemYOffsets []int, contentX int, boxWidth int) {
	var builder strings.Builder
	contentY := 0
	if menu.Title != "" {
		builder.WriteString(styles.CardTitle.Render(menu.Title))
		builder.WriteByte('\n')
		contentY++
	}
	itemYOffsets = make([]int, 0, len(menu.Items))
	for index, item := range menu.Items {
		itemYOffsets = append(itemYOffsets, contentY+index)
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
	rendered = styles.Card.Render(builder.String())
	contentX = styles.Card.GetBorderLeftSize() + styles.Card.GetPaddingLeft()
	contentTop := styles.Card.GetBorderTopSize() + styles.Card.GetPaddingTop()
	for index := range itemYOffsets {
		itemYOffsets[index] += contentTop
	}
	boxWidth = lipgloss.Width(rendered)
	return rendered, itemYOffsets, contentX, boxWidth
}
