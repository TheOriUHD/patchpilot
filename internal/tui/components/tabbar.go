package components

import (
	"strings"

	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

// TabBarState describes the top-level tabs to render.
type TabBarState struct {
	Labels []string
	Active int
}

// RenderTabBar renders the global tab bar.
func RenderTabBar(styles theme.Styles, state TabBarState) string {
	rendered := make([]string, 0, len(state.Labels))
	for index, label := range state.Labels {
		text := "[ " + label + " ]"
		if index == state.Active {
			rendered = append(rendered, styles.ActiveTab.Render(text))
			continue
		}
		rendered = append(rendered, styles.Tab.Render(text))
	}
	return strings.Join(rendered, " ")
}
