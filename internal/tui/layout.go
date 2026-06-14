package tui

// Rect describes a terminal-cell rectangle for hit testing.
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Contains reports whether x,y is inside the rectangle.
func (r Rect) Contains(x int, y int) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.Width && y < r.Y+r.Height
}

// Empty reports whether the rectangle has no area.
func (r Rect) Empty() bool {
	return r.Width <= 0 || r.Height <= 0
}
