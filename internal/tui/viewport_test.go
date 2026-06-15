package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderClampedLinesPadsShortContent(t *testing.T) {
	t.Parallel()

	lines := RenderClampedLines(ViewportInput{Lines: []string{"one", "two"}, Width: 10, Height: 4})
	if len(lines) != 4 {
		t.Fatalf("len = %d, want 4", len(lines))
	}
	if lines[0] != "one" || lines[1] != "two" || lines[2] != "" || lines[3] != "" {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestRenderClampedLinesTailsByDefault(t *testing.T) {
	t.Parallel()

	lines := RenderClampedLines(ViewportInput{Lines: []string{"one", "two", "three", "four"}, Width: 10, Height: 2})
	if got, want := lines, []string{"three", "four"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
}

func TestRenderClampedLinesScrollOffset(t *testing.T) {
	t.Parallel()

	lines := RenderClampedLines(ViewportInput{Lines: []string{"one", "two", "three", "four"}, Width: 10, Height: 2, ScrollOffset: 1})
	if got, want := lines, []string{"two", "three"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
}

func TestRenderClampedLinesClampsDisplayWidth(t *testing.T) {
	t.Parallel()

	lines := RenderClampedLines(ViewportInput{Lines: []string{"abcdefghijklmnopqrstuvwxyz"}, Width: 8, Height: 1})
	if got := lipgloss.Width(lines[0]); got > 8 {
		t.Fatalf("width = %d, want <= 8 for %q", got, lines[0])
	}
}

func TestRenderClampedLinesHandlesUnicodeWidth(t *testing.T) {
	t.Parallel()

	lines := RenderClampedLines(ViewportInput{Lines: []string{"界界界abc"}, Width: 5, Height: 1})
	if got := lipgloss.Width(lines[0]); got > 5 {
		t.Fatalf("width = %d, want <= 5 for %q", got, lines[0])
	}
}

func TestRenderClampedLinesStripsANSI(t *testing.T) {
	t.Parallel()

	lines := RenderClampedLines(ViewportInput{Lines: []string{"\x1b[31mred text\x1b[0m"}, Width: 20, Height: 1})
	if lines[0] != "red text" {
		t.Fatalf("line = %q, want ANSI stripped text", lines[0])
	}
}
