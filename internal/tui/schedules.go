package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/ipc"
	"github.com/TheOriUHD/patchpilot/internal/scheduler"
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

type scheduleScreen int

const (
	scheduleScreenList scheduleScreen = iota
	scheduleScreenEditor
)

type scheduleEditor struct {
	id         int64
	input      core.ScheduleInput
	field      int
	hostCursor int
}

type schedulePendingAction struct {
	Kind string
	ID   int64
	Name string
}

type schedulesLoadedMsg struct {
	schedules []core.Schedule
	err       error
}

type scheduleMutationMsg struct {
	message string
	err     error
}

func (m model) handleSchedulesKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.scheduleScreen == scheduleScreenEditor {
		return m.handleScheduleEditorKey(key)
	}
	if m.schedulePending.Kind != "" {
		switch key.String() {
		case "y", "enter":
			action := m.schedulePending
			m.schedulePending = schedulePendingAction{}
			if action.Kind == "delete" {
				return m, withClearScreen(deleteScheduleCmd(m.ctx, m.client, action.ID))
			}
			if action.Kind == "run" {
				return m, withClearScreen(runScheduleCmd(m.ctx, m.client, action.ID))
			}
		case "n", "esc":
			m.schedulePending = schedulePendingAction{}
			m.status = "Cancelled"
			return m, clearScreenOnTransition()
		}
		return m, nil
	}
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.activeTab = (m.activeTab + 1) % len(m.tabs)
		return m, nil
	case "shift+tab":
		m.activeTab = (m.activeTab + len(m.tabs) - 1) % len(m.tabs)
		return m, nil
	case "1", "2", "3", "4":
		index := int(key.String()[0] - '1')
		if index >= 0 && index < len(m.tabs) {
			m.activeTab = index
		}
		return m, nil
	case "r":
		m.scheduleLoading = true
		return m, loadSchedulesCmd(m.ctx, m.client)
	case "a":
		m.scheduleScreen = scheduleScreenEditor
		m.scheduleEditor = scheduleEditor{input: m.defaultScheduleInput()}
		m.status = "New schedule"
		return m, clearScreenOnTransition()
	case "e", "enter":
		if schedule, ok := m.selectedSchedule(); ok {
			m.scheduleScreen = scheduleScreenEditor
			m.scheduleEditor = scheduleEditor{id: schedule.ID, input: scheduler.InputFromSchedule(schedule)}
			m.status = "Edit schedule"
			return m, clearScreenOnTransition()
		}
		return m, nil
	case "x":
		if schedule, ok := m.selectedSchedule(); ok {
			m.schedulePending = schedulePendingAction{Kind: "delete", ID: schedule.ID, Name: schedule.Name}
			return m, clearScreenOnTransition()
		}
		return m, nil
	case "R":
		if schedule, ok := m.selectedSchedule(); ok {
			m.schedulePending = schedulePendingAction{Kind: "run", ID: schedule.ID, Name: schedule.Name}
			return m, clearScreenOnTransition()
		}
		return m, nil
	case "m":
		if _, ok := m.selectedSchedule(); ok {
			return m.openScheduleContextMenu(m.scheduleSelected, 4, m.renderBodyY+5+m.scheduleSelected), clearScreenOnTransition()
		}
		return m, nil
	case "down", "j":
		m = m.clearHover()
		if m.scheduleSelected < len(m.schedules)-1 {
			m.scheduleSelected++
		}
		return m, nil
	case "up", "k":
		m = m.clearHover()
		if m.scheduleSelected > 0 {
			m.scheduleSelected--
		}
		return m, nil
	}
	return m, nil
}

func (m model) handleScheduleEditorKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	fields := m.scheduleEditorFields()
	if m.scheduleEditor.field >= len(fields) {
		m.scheduleEditor.field = len(fields) - 1
	}
	switch key.String() {
	case "esc":
		m.scheduleScreen = scheduleScreenList
		m.status = "Cancelled"
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "tab", "down":
		if m.scheduleEditor.field < len(fields)-1 {
			m.scheduleEditor.field++
		}
		return m, nil
	case "shift+tab", "up":
		if m.scheduleEditor.field > 0 {
			m.scheduleEditor.field--
		}
		return m, nil
	case "enter", "s":
		if m.scheduleEditor.field == len(fields)-1 || key.String() == "s" {
			return m, saveScheduleCmd(m.ctx, m.client, m.scheduleEditor.id, m.scheduleEditor.input)
		}
		m.scheduleEditor.field++
		return m, nil
	case "backspace":
		if fields[m.scheduleEditor.field] == "Name" && len(m.scheduleEditor.input.Name) > 0 {
			m.scheduleEditor.input.Name = m.scheduleEditor.input.Name[:len(m.scheduleEditor.input.Name)-1]
		}
		return m, nil
	case "left", "h":
		m.adjustScheduleField(-1)
		return m, nil
	case "right", "l":
		m.adjustScheduleField(1)
		return m, nil
	case " ":
		if fields[m.scheduleEditor.field] == "Reboot" {
			m.scheduleEditor.input.SuppressReboot = !m.scheduleEditor.input.SuppressReboot
		}
		if fields[m.scheduleEditor.field] == "Hosts" && len(m.hosts) > 0 {
			host := m.hosts[m.scheduleEditor.hostCursor]
			m.scheduleEditor.input.HostIDs = toggleID(m.scheduleEditor.input.HostIDs, host.ID)
		}
		return m, nil
	}
	if fields[m.scheduleEditor.field] == "Name" && len(key.Runes) > 0 {
		m.scheduleEditor.input.Name += string(key.Runes)
	}
	if fields[m.scheduleEditor.field] == "Hosts" {
		switch key.String() {
		case "j":
			if m.scheduleEditor.hostCursor < len(m.hosts)-1 {
				m.scheduleEditor.hostCursor++
			}
		case "k":
			if m.scheduleEditor.hostCursor > 0 {
				m.scheduleEditor.hostCursor--
			}
		}
	}
	return m, nil
}

func (m *model) adjustScheduleField(delta int) {
	fields := m.scheduleEditorFields()
	if m.scheduleEditor.field >= len(fields) {
		m.scheduleEditor.field = len(fields) - 1
	}
	field := fields[m.scheduleEditor.field]
	switch field {
	case "Frequency":
		frequencies := []core.Frequency{core.FrequencyHourly, core.FrequencyDaily, core.FrequencyWeekly, core.FrequencyMonthly}
		index := 0
		for i, frequency := range frequencies {
			if frequency == m.scheduleEditor.input.Frequency {
				index = i
			}
		}
		index = (index + delta + len(frequencies)) % len(frequencies)
		m.scheduleEditor.input.Frequency = frequencies[index]
	case "Hour":
		m.scheduleEditor.input.Hour = clamp(m.scheduleEditor.input.Hour+delta, 0, 23)
	case "Minute":
		m.scheduleEditor.input.Minute = clamp(m.scheduleEditor.input.Minute+delta, 0, 59)
	case "Day":
		switch m.scheduleEditor.input.Frequency {
		case core.FrequencyWeekly:
			m.scheduleEditor.input.DayOfWeek = clamp(m.scheduleEditor.input.DayOfWeek+delta, 0, 6)
		case core.FrequencyMonthly:
			m.scheduleEditor.input.DayOfMonth = clamp(m.scheduleEditor.input.DayOfMonth+delta, 1, 28)
		}
	case "Enabled":
		m.scheduleEditor.input.Enabled = !m.scheduleEditor.input.Enabled
	case "Reboot":
		m.scheduleEditor.input.SuppressReboot = !m.scheduleEditor.input.SuppressReboot
	case "Max":
		m.scheduleEditor.input.MaxConcurrent = clamp(max(1, m.scheduleEditor.input.MaxConcurrent)+delta, 1, 10)
	case "Hosts":
		if len(m.hosts) > 0 {
			m.scheduleEditor.hostCursor = clamp(m.scheduleEditor.hostCursor+delta, 0, len(m.hosts)-1)
		}
	}
}

func (m model) renderSchedulesView(bodyHeight int) string {
	if m.scheduleLoading {
		return m.centerCard(bodyHeight, m.styles.Card.Render(m.spinner.View()+" Loading schedules"))
	}
	if m.scheduleError != "" {
		return m.centerCard(bodyHeight, m.styles.Card.Width(64).Render(m.styles.ErrorText.Render("Could not load schedules")+"\n\n"+m.scheduleError))
	}
	if m.scheduleScreen == scheduleScreenEditor {
		return m.renderScheduleEditor(bodyHeight)
	}
	if len(m.schedules) == 0 {
		return m.centerCard(bodyHeight, m.styles.Card.Width(58).Render(m.styles.CardTitle.Render("Schedules")+"\n\nNo schedules yet - press a."))
	}
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Schedules"))
	builder.WriteString("\n\n")
	fmt.Fprintf(&builder, "%-2s %-24s %-10s %-8s %-7s %-18s %s\n", "", "Name", "Freq", "Enabled", "Hosts", "Next", "Last skip")
	builder.WriteString(m.styles.MutedText.Render(strings.Repeat("─", 92)))
	builder.WriteByte('\n')
	for index, schedule := range m.schedules {
		rowY := m.renderBodyY + 5 + index
		if m.regions != nil {
			m.regions.Add(region{ID: fmt.Sprintf("schedule-row:%d", index), Rect: Rect{X: 2, Y: rowY, Width: 92, Height: 1}, Action: regionActionScheduleRow, Index: index})
		}
		cursor := " "
		if index == m.scheduleSelected {
			cursor = theme.SymbolPointer
		}
		next := formatMaybeTime(schedule.NextRunAt)
		skip := "-"
		if schedule.LastSkippedAt != nil {
			skip = "overlap"
		}
		line := fmt.Sprintf("%-2s %-24s %-10s %-8v %-7d %-18s %s", cursor, truncate(schedule.Name, 24), schedule.Frequency, schedule.Enabled, schedule.HostCount, truncate(next, 18), skip)
		switch index {
		case m.scheduleSelected:
			builder.WriteString(m.styles.ActiveTab.Render(line))
		case m.hoveredSchedule:
			builder.WriteString(components.RenderListRow(m.styles, components.Row{Text: line, Hover: true}))
		default:
			builder.WriteString(line)
		}
		builder.WriteByte('\n')
	}
	if schedule, ok := m.selectedSchedule(); ok {
		builder.WriteString("\n")
		builder.WriteString(m.styles.CardTitle.Render("Next three"))
		builder.WriteString("  ")
		preview, err := scheduler.NextRuns(scheduler.InputFromSchedule(schedule), time.Now().UTC(), 3)
		if err != nil {
			builder.WriteString(m.styles.ErrorText.Render(err.Error()))
		} else {
			parts := make([]string, 0, len(preview))
			for _, value := range preview {
				parts = append(parts, value.Format("Jan 02 15:04"))
			}
			builder.WriteString(strings.Join(parts, "  "))
		}
	}
	content := lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Padding(1, 2).Render(builder.String())
	if m.schedulePending.Kind != "" {
		content = m.renderCenteredOverlay(content, m.renderScheduleConfirm(), bodyHeight)
	}
	return content
}

func (m model) renderScheduleConfirm() string {
	message := "Confirm? y/n"
	if m.schedulePending.Kind == "delete" {
		message = fmt.Sprintf("Delete schedule %s? y/n", m.schedulePending.Name)
	}
	if m.schedulePending.Kind == "run" {
		message = fmt.Sprintf("Run schedule %s now? y/n", m.schedulePending.Name)
	}
	return m.styles.Card.Width(72).Render(message)
}

func (m model) renderScheduleEditor(bodyHeight int) string {
	fields := m.scheduleEditorFields()
	if m.scheduleEditor.field >= len(fields) {
		m.scheduleEditor.field = len(fields) - 1
	}
	input := m.scheduleEditor.input
	values := map[string]string{
		"Name":      input.Name,
		"Frequency": string(input.Frequency),
		"Hour":      fmt.Sprintf("%02d", input.Hour),
		"Minute":    fmt.Sprintf("%02d", input.Minute),
		"Day":       scheduleDayValue(input),
		"Max":       strconv.Itoa(max(1, input.MaxConcurrent)),
		"Enabled":   strconv.FormatBool(input.Enabled),
		"Reboot":    suppressRebootLabel(input.SuppressReboot),
		"Hosts":     fmt.Sprintf("%d selected", len(input.HostIDs)),
		"Save":      "press Enter",
	}
	var builder strings.Builder
	title := "New schedule"
	if m.scheduleEditor.id != 0 {
		title = "Edit schedule"
	}
	builder.WriteString(m.styles.CardTitle.Render(title))
	builder.WriteString("\n\n")
	for index, field := range fields {
		line := fmt.Sprintf("%-10s %s", field+":", values[field])
		if index == m.scheduleEditor.field {
			builder.WriteString(m.styles.ActiveTab.Render(line))
		} else {
			builder.WriteString(line)
		}
		builder.WriteByte('\n')
	}
	builder.WriteString(m.styles.MutedText.Render("When enabled, host reboot policy is ignored for runs from this schedule; reboots are suppressed, never forced."))
	builder.WriteByte('\n')
	builder.WriteString("\n")
	builder.WriteString(m.styles.CardTitle.Render("Hosts"))
	builder.WriteByte('\n')
	if len(m.hosts) == 0 {
		builder.WriteString("No hosts available.\n")
	} else {
		for index, host := range m.hosts {
			cursor := " "
			if index == m.scheduleEditor.hostCursor && fields[m.scheduleEditor.field] == "Hosts" {
				cursor = theme.SymbolPointer
			}
			checked := "[ ]"
			if hasID(input.HostIDs, host.ID) {
				checked = "[x]"
			}
			fmt.Fprintf(&builder, "%s %s %-24s %s\n", cursor, checked, truncate(host.Name, 24), host.PackageManager)
		}
	}
	builder.WriteString("\n")
	builder.WriteString(m.styles.CardTitle.Render("Preview"))
	builder.WriteString("  ")
	preview, err := scheduler.NextRuns(input, time.Now().UTC(), 3)
	if err != nil {
		builder.WriteString(m.styles.ErrorText.Render(err.Error()))
	} else {
		parts := make([]string, 0, len(preview))
		for _, value := range preview {
			parts = append(parts, value.Format("Jan 02 15:04"))
		}
		builder.WriteString(strings.Join(parts, "  "))
	}
	return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Padding(1, 2).Render(builder.String())
}

func (m model) scheduleKeybinds() []string {
	if m.scheduleScreen == scheduleScreenEditor {
		return []string{"Tab field", "←/→ change", "Space toggle", "s save", "Esc cancel"}
	}
	if m.schedulePending.Kind != "" {
		return []string{"y confirm", "n cancel"}
	}
	return []string{"↑/↓ navigate", "a add", "Enter edit", "R run", "x remove", "r refresh", "m menu"}
}

func (m model) selectedSchedule() (core.Schedule, bool) {
	if m.scheduleSelected < 0 || m.scheduleSelected >= len(m.schedules) {
		return core.Schedule{}, false
	}
	return m.schedules[m.scheduleSelected], true
}

func (m model) defaultScheduleInput() core.ScheduleInput {
	hostIDs := make([]int64, 0)
	for _, host := range m.hosts {
		if host.Enabled {
			hostIDs = append(hostIDs, host.ID)
		}
	}
	return core.ScheduleInput{Name: "Weekly maintenance", Frequency: core.FrequencyWeekly, Hour: 3, Minute: 0, DayOfWeek: 0, DayOfMonth: 1, Enabled: true, HostIDs: hostIDs, MaxConcurrent: 1}
}

func loadSchedulesCmd(ctx context.Context, client *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		schedules, err := client.ListSchedules(requestCtx)
		return schedulesLoadedMsg{schedules: schedules, err: err}
	}
}

func saveScheduleCmd(ctx context.Context, client *ipc.Client, id int64, input core.ScheduleInput) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var err error
		if id == 0 {
			_, err = client.CreateSchedule(requestCtx, input)
		} else {
			_, err = client.UpdateSchedule(requestCtx, id, input)
		}
		message := "Schedule saved"
		return scheduleMutationMsg{message: message, err: err}
	}
}

func deleteScheduleCmd(ctx context.Context, client *ipc.Client, id int64) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		err := client.DeleteSchedule(requestCtx, id)
		return scheduleMutationMsg{message: "Schedule removed", err: err}
	}
}

func runScheduleCmd(ctx context.Context, client *ipc.Client, id int64) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		detail, err := client.RunScheduleNow(requestCtx, id)
		return runStartedMsg{detail: detail, err: err}
	}
}

func (m model) scheduleEditorFields() []string {
	fields := []string{"Name", "Frequency", "Hour", "Minute", "Day", "Enabled", "Reboot"}
	if m.settingsSnapshot != nil && m.settingsSnapshot.ConcurrencyEnabled {
		fields = append(fields, "Max")
	}
	return append(fields, "Hosts", "Save")
}

func suppressRebootLabel(value bool) string {
	if value {
		return "[x] Suppress automatic reboot for this schedule"
	}
	return "[ ] Suppress automatic reboot for this schedule"
}

func scheduleDayValue(input core.ScheduleInput) string {
	switch input.Frequency {
	case core.FrequencyWeekly:
		return fmt.Sprintf("weekday %d", input.DayOfWeek)
	case core.FrequencyMonthly:
		return fmt.Sprintf("day %d", input.DayOfMonth)
	default:
		return "-"
	}
}

func formatMaybeTime(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.Format("Jan 02 15:04")
}

func toggleID(ids []int64, id int64) []int64 {
	for index, existing := range ids {
		if existing == id {
			return append(ids[:index], ids[index+1:]...)
		}
	}
	return append(ids, id)
}

func hasID(ids []int64, id int64) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}

func clamp(value int, low int, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
