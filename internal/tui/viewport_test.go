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

func TestRenderClampedLinesCollapsesCarriageReturnProgress(t *testing.T) {
	t.Parallel()

	input := "(Reading database ... 5%\r(Reading database ... 55%\r(Reading database ... 100%)"
	lines := RenderClampedLines(ViewportInput{Lines: []string{input}, Width: 80, Height: 1})
	if got, want := lines[0], "(Reading database ... 100%)"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

func TestRenderClampedLinesDropsMalformedResetFragments(t *testing.T) {
	t.Parallel()

	lines := RenderClampedLines(ViewportInput{Lines: []string{"configuring.0m", "[0m", "\x1b[32mok\x1b[0m"}, Width: 40, Height: 3})
	if lines[0] != "configuring." || lines[1] != "" || lines[2] != "ok" {
		t.Fatalf("lines = %#v, want reset fragments stripped", lines)
	}
}
