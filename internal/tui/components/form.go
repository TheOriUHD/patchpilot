package components

import (
	"fmt"

	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

// Checkbox renders a boolean field.
type Checkbox struct {
	Label   string
	Checked bool
	Focus   bool
}

func RenderCheckbox(styles theme.Styles, checkbox Checkbox) string {
	mark := "[ ]"
	if checkbox.Checked {
		mark = "[x]"
	}
	text := mark + " " + checkbox.Label
	if checkbox.Focus {
		return styles.ActiveTab.Render(text)
	}
	return text
}

// NumericInput renders a bounded integer field.
type NumericInput struct {
	Label string
	Value int
	Min   int
	Max   int
	Focus bool
}

func ClampNumeric(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func RenderNumericInput(styles theme.Styles, input NumericInput) string {
	value := ClampNumeric(input.Value, input.Min, input.Max)
	text := fmt.Sprintf("%s [ %d ]", input.Label, value)
	if input.Focus {
		return styles.ActiveTab.Render(text)
	}
	return text
}

// TextInput renders a focused or unfocused single-line text field.
type TextInput struct {
	Label string
	Value string
	Focus bool
}

func RenderTextInput(styles theme.Styles, input TextInput) string {
	text := fmt.Sprintf("%s [%s]", input.Label, input.Value)
	if input.Focus {
		return styles.ActiveTab.Render(text)
	}
	return text
}

// RadioOption describes one radio button option.
type RadioOption struct {
	Label    string
	Selected bool
}

func RenderRadioOption(styles theme.Styles, option RadioOption, focus bool) string {
	mark := "( )"
	if option.Selected {
		mark = "(•)"
	}
	text := mark + " " + option.Label
	if focus {
		return styles.ActiveTab.Render(text)
	}
	return text
}
