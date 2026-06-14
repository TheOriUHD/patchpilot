package components

import "github.com/charmbracelet/lipgloss"

// RenderFullCellSurface forces a rendered view to occupy the full terminal frame.
func RenderFullCellSurface(width int, height int, content string) string {
	return lipgloss.NewStyle().Width(width).Height(height).Render(content)
}

// PlaceModal centers content and fills surrounding cells with spaces.
func PlaceModal(width int, height int, content string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content, lipgloss.WithWhitespaceChars(" "))
}
