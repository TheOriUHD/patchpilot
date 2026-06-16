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

// ContextMenuItemRect describes one item hit row relative to a rendered menu block.
type ContextMenuItemRect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// ContextMenuLayout contains a rendered menu block and item geometry inside it.
type ContextMenuLayout struct {
	Rendered  string
	ItemRects []ContextMenuItemRect
	BoxWidth  int
	BoxHeight int
}

// RenderContextMenu renders a menu for future mouse/right-click workflows.
func RenderContextMenu(styles theme.Styles, menu ContextMenu) string {
	return RenderContextMenuLayout(styles, menu).Rendered
}

// RenderContextMenuLayout renders a menu and returns item geometry relative to the box top-left.
func RenderContextMenuLayout(styles theme.Styles, menu ContextMenu) ContextMenuLayout {
	var builder strings.Builder
	contentY := 0
	if menu.Title != "" {
		builder.WriteString(styles.CardTitle.Render(menu.Title))
		builder.WriteByte('\n')
		contentY++
	}
	itemRows := make([]int, 0, len(menu.Items))
	for index, item := range menu.Items {
		itemRows = append(itemRows, contentY+index)
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
	rendered := styles.Card.Render(builder.String())
	boxWidth := lipgloss.Width(rendered)
	boxHeight := lipgloss.Height(rendered)
	contentX := styles.Card.GetBorderLeftSize() + styles.Card.GetPaddingLeft()
	contentTop := styles.Card.GetBorderTopSize() + styles.Card.GetPaddingTop()
	contentWidth := boxWidth - contentX - styles.Card.GetBorderRightSize()
	if contentWidth < 1 {
		contentWidth = boxWidth
	}
	itemRects := make([]ContextMenuItemRect, 0, len(itemRows))
	for _, row := range itemRows {
		itemRects = append(itemRects, ContextMenuItemRect{X: contentX, Y: contentTop + row, Width: contentWidth, Height: 1})
	}
	return ContextMenuLayout{Rendered: rendered, ItemRects: itemRects, BoxWidth: boxWidth, BoxHeight: boxHeight}
}
