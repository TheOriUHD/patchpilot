package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ViewportInput describes a bounded terminal-cell viewport.
type ViewportInput struct {
	Lines        []string
	Width        int
	Height       int
	ScrollOffset int
}

// RenderClampedLines returns exactly Height lines, each no wider than Width.
func RenderClampedLines(input ViewportInput) []string {
	if input.Height <= 0 {
		return nil
	}
	output := make([]string, 0, input.Height)
	if input.Width <= 0 {
		for len(output) < input.Height {
			output = append(output, "")
		}
		return output
	}

	lines := make([]string, 0, len(input.Lines))
	for _, line := range input.Lines {
		lines = append(lines, clampLineWidth(cleanViewportLine(line), input.Width))
	}

	start := 0
	if len(lines) > input.Height {
		offset := input.ScrollOffset
		if offset < 0 {
			offset = 0
		}
		start = len(lines) - input.Height - offset
		if start < 0 {
			start = 0
		}
	}
	end := min(len(lines), start+input.Height)
	output = append(output, lines[start:end]...)
	for len(output) < input.Height {
		output = append(output, "")
	}
	return output
}

func cleanViewportLine(line string) string {
	cleaned := stripANSISequences(line)
	cleaned = collapseCarriageReturns(cleaned)
	return stripANSIRemnants(cleaned)
}

func collapseCarriageReturns(line string) string {
	if !strings.ContainsRune(line, '\r') {
		return line
	}
	parts := strings.Split(line, "\r")
	for index := len(parts) - 1; index >= 0; index-- {
		if parts[index] != "" {
			return parts[index]
		}
	}
	return ""
}

func stripANSIRemnants(line string) string {
	cleaned := strings.ReplaceAll(line, "[0m", "")
	cleaned = strings.ReplaceAll(cleaned, "[39m", "")
	cleaned = strings.ReplaceAll(cleaned, "[49m", "")
	cleaned = strings.ReplaceAll(cleaned, ".0m", ".")
	if cleaned == "0m" {
		return ""
	}
	return cleaned
}

func clampLineWidth(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(line) <= width {
		return line
	}
	var builder strings.Builder
	for _, r := range line {
		candidate := builder.String() + string(r)
		if lipgloss.Width(candidate) > width {
			break
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func stripANSISequences(value string) string {
	var builder strings.Builder
	inEscape := false
	for index := 0; index < len(value); index++ {
		char := value[index]
		if inEscape {
			if char == '[' {
				continue
			}
			if char >= '@' && char <= '~' {
				inEscape = false
			}
			continue
		}
		if char == 0x1b {
			inEscape = true
			continue
		}
		builder.WriteByte(char)
	}
	return builder.String()
}
