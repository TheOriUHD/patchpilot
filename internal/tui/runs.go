package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/ipc"
	"github.com/TheOriUHD/patchpilot/internal/stream"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

type runsLoadedMsg struct {
	runs []core.Run
	err  error
}

type runStartedMsg struct {
	detail core.RunDetail
	err    error
}

type runDetailLoadedMsg struct {
	detail core.RunDetail
	logs   []core.RunLogEntry
	err    error
}

type runStreamMsg struct {
	event stream.Event
}

type runStreamErrorMsg struct {
	err error
}

type runCancelledMsg struct {
	err error
}

type liveLogEntry struct {
	hostRunID int64
	hostName  string
	line      string
}

func (m model) handleRunsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.liveCancel {
		switch key.String() {
		case "y", "enter":
			if m.liveRun == nil {
				m.liveCancel = false
				return m, nil
			}
			return m, cancelRunCmd(m.ctx, m.client, m.liveRun.Run.ID)
		case "n", "esc":
			m.liveCancel = false
			m.status = "Cancellation aborted"
			return m, nil
		}
		return m, nil
	}
	if m.liveRun != nil {
		switch key.String() {
		case "q", "esc":
			if m.runStreamStop != nil {
				m.runStreamStop()
				m.runStreamStop = nil
			}
			m.liveRun = nil
			m.liveLogLines = nil
			m.status = "Left live run"
			return m, nil
		case "tab":
			if len(m.liveRun.HostRuns) > 0 {
				m.liveHostIndex = (m.liveHostIndex + 1) % len(m.liveRun.HostRuns)
			}
			return m, nil
		case "shift+tab":
			if len(m.liveRun.HostRuns) > 0 {
				m.liveHostIndex = (m.liveHostIndex + len(m.liveRun.HostRuns) - 1) % len(m.liveRun.HostRuns)
			}
			return m, nil
		case "K":
			if m.historicalRun {
				return m, nil
			}
			m.liveCancel = true
			m.status = "Confirm run cancellation"
			return m, nil
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
		m.runLoading = true
		return m, loadRunsPageCmd(m.ctx, m.client, m.runListQuery())
	case "o":
		m.runOffset += 100
		m.runLoading = true
		return m, loadRunsPageCmd(m.ctx, m.client, m.runListQuery())
	case "f":
		m.runDateRange = nextRunDateRange(m.runDateRange)
		m.runOffset = 0
		m.runLoading = true
		return m, loadRunsPageCmd(m.ctx, m.client, m.runListQuery())
	case "S":
		m.toggleRunStatusFilter(core.RunStatusSuccess)
		m.runLoading = true
		return m, loadRunsPageCmd(m.ctx, m.client, m.runListQuery())
	case "F":
		m.toggleRunStatusFilter(core.RunStatusFailed)
		m.runLoading = true
		return m, loadRunsPageCmd(m.ctx, m.client, m.runListQuery())
	case "P":
		m.toggleRunStatusFilter(core.RunStatusRunning)
		m.runLoading = true
		return m, loadRunsPageCmd(m.ctx, m.client, m.runListQuery())
	case "M":
		m.toggleRunTriggerFilter(core.RunTriggerManual)
		m.runLoading = true
		return m, loadRunsPageCmd(m.ctx, m.client, m.runListQuery())
	case "A":
		m.toggleRunTriggerFilter(core.RunTriggerScheduled)
		m.runLoading = true
		return m, loadRunsPageCmd(m.ctx, m.client, m.runListQuery())
	case "C":
		m.toggleRunTriggerFilter(core.RunTriggerCLI)
		m.runLoading = true
		return m, loadRunsPageCmd(m.ctx, m.client, m.runListQuery())
	case "enter":
		if run, ok := m.selectedRun(); ok {
			return m, loadRunDetailCmd(m.ctx, m.client, run.ID)
		}
		return m, nil
	case "down", "j":
		if m.runSelected < len(m.runs)-1 {
			m.runSelected++
		}
		return m, nil
	case "up", "k":
		if m.runSelected > 0 {
			m.runSelected--
		}
		return m, nil
	}
	return m, nil
}

func (m model) renderRunsView(bodyHeight int) string {
	if m.liveRun != nil {
		return m.renderLiveRun(bodyHeight)
	}
	if m.runLoading {
		return m.centerCard(bodyHeight, m.styles.Card.Render(m.spinner.View()+" Loading runs"))
	}
	if m.runError != "" {
		return m.centerCard(bodyHeight, m.styles.Card.Width(64).Render(m.styles.ErrorText.Render("Could not load runs")+"\n\n"+m.runError))
	}
	if len(m.runs) == 0 {
		return m.centerCard(bodyHeight, m.styles.Card.Width(58).Render(m.styles.CardTitle.Render("Runs")+"\n\nNo runs have been recorded yet."))
	}
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Runs"))
	builder.WriteString("  ")
	builder.WriteString(m.styles.MutedText.Render(m.runFilterSummary()))
	builder.WriteString("\n\n")
	builder.WriteString(fmt.Sprintf("%-2s %-6s %-18s %-11s %-13s %-7s %-8s %s\n", "", "ID", "Started", "Trigger", "Status", "Hosts", "Dur", "Schedule"))
	builder.WriteString(m.styles.MutedText.Render(strings.Repeat("─", 94)))
	builder.WriteByte('\n')
	for index, run := range m.runs {
		cursor := " "
		if index == m.runSelected {
			cursor = theme.SymbolPointer
		}
		line := fmt.Sprintf("%-2s %-6d %-18s %-11s %-13s %-7d %-8s %s", cursor, run.ID, run.StartedAt.Format("Jan 02 15:04"), run.Trigger, run.Status, run.HostCount, formatDuration(run.DurationSeconds), truncate(run.ScheduleName, 24))
		if index == m.runSelected {
			builder.WriteString(m.styles.ActiveTab.Render(line))
		} else {
			builder.WriteString(line)
		}
		builder.WriteByte('\n')
	}
	return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Padding(1, 2).Render(builder.String())
}

func (m model) renderLiveRun(bodyHeight int) string {
	detail := *m.liveRun
	var builder strings.Builder
	title := fmt.Sprintf("Run %d", detail.Run.ID)
	if m.historicalRun {
		title += " history"
	}
	builder.WriteString(m.styles.CardTitle.Render(title))
	builder.WriteString("  ")
	builder.WriteString(string(detail.Run.Status))
	if m.liveCancel {
		builder.WriteString("  ")
		builder.WriteString(m.styles.ErrorText.Render("Cancel this run? y/n"))
	}
	builder.WriteString("\n\n")
	builder.WriteString(fmt.Sprintf("%-2s %-24s %-12s %-8s %s\n", "", "Host", "Status", "Reboot", "Message"))
	builder.WriteString(m.styles.MutedText.Render(strings.Repeat("─", 86)))
	builder.WriteByte('\n')
	for index, hostRun := range detail.HostRuns {
		cursor := " "
		if index == m.liveHostIndex {
			cursor = theme.SymbolPointer
		}
		reboot := "-"
		if hostRun.RebootRequired {
			reboot = "yes"
		}
		message := hostRun.ErrorSummary
		line := fmt.Sprintf("%-2s %-24s %-12s %-8s %s", cursor, truncate(hostRun.HostName, 24), hostRun.Status, reboot, truncate(message, 34))
		if index == m.liveHostIndex {
			builder.WriteString(m.styles.ActiveTab.Render(line))
		} else {
			builder.WriteString(line)
		}
		builder.WriteByte('\n')
	}
	builder.WriteString("\n")
	selectedHostRunID := int64(0)
	selectedHost := "all hosts"
	if len(detail.HostRuns) > 0 {
		selected := detail.HostRuns[m.liveHostIndex]
		selectedHostRunID = selected.ID
		selectedHost = selected.HostName
	}
	builder.WriteString(m.styles.CardTitle.Render("Log tail"))
	builder.WriteString("  ")
	builder.WriteString(selectedHost)
	builder.WriteByte('\n')
	logLines := m.filteredLiveLogLines(selectedHostRunID)
	start := max(0, len(logLines)-max(6, bodyHeight/2))
	for _, entry := range logLines[start:] {
		builder.WriteString(entry.line)
		builder.WriteByte('\n')
	}
	return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Padding(1, 2).Render(builder.String())
}

func (m model) filteredLiveLogLines(hostRunID int64) []liveLogEntry {
	if hostRunID == 0 {
		return m.liveLogLines
	}
	filtered := make([]liveLogEntry, 0)
	for _, entry := range m.liveLogLines {
		if entry.hostRunID == hostRunID {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (m model) runKeybinds() []string {
	if m.liveCancel {
		return []string{"y cancel", "n keep running"}
	}
	if m.liveRun != nil {
		if m.historicalRun {
			return []string{"Tab host log", "q back"}
		}
		return []string{"Tab host log", "K cancel", "q back"}
	}
	return []string{"↑/↓ navigate", "Enter open", "f range", "S/F/P status", "M/A/C trigger", "o older"}
}

func (m model) selectedRun() (core.Run, bool) {
	if m.runSelected < 0 || m.runSelected >= len(m.runs) {
		return core.Run{}, false
	}
	return m.runs[m.runSelected], true
}

func (m *model) startRunStream(runID int64) tea.Cmd {
	if m.runStreamStop != nil {
		m.runStreamStop()
	}
	streamCtx, cancel := context.WithCancel(m.ctx)
	m.runStreamStop = cancel
	events, errors := m.client.RunEvents(streamCtx, runID)
	m.runEvents = events
	m.runEventErrors = errors
	return waitRunEventCmd(events, errors)
}

func (m *model) handleRunStreamEvent(event stream.Event) tea.Cmd {
	switch event.Type {
	case "run.event":
		var runEvent core.RunEvent
		if err := json.Unmarshal(event.Data, &runEvent); err == nil {
			m.applyRunEvent(runEvent)
		}
	case "log.line":
		var line stream.LogLine
		if err := json.Unmarshal(event.Data, &line); err == nil && line.Line != "" {
			m.liveLogLines = append(m.liveLogLines, liveLogEntry{hostRunID: line.HostRunID, hostName: line.HostName, line: line.Line})
			if len(m.liveLogLines) > 1000 {
				m.liveLogLines = m.liveLogLines[len(m.liveLogLines)-1000:]
			}
		}
	}
	return waitRunEventCmd(m.runEvents, m.runEventErrors)
}

func (m *model) applyRunEvent(event core.RunEvent) {
	if m.liveRun == nil {
		return
	}
	if event.RunStatus != "" {
		m.liveRun.Run.Status = event.RunStatus
		m.liveRun.Run.Summary = event.Message
		m.status = event.Message
	}
	if event.HostRunID == 0 {
		return
	}
	for index := range m.liveRun.HostRuns {
		if m.liveRun.HostRuns[index].ID != event.HostRunID {
			continue
		}
		if event.HostStatus != "" {
			m.liveRun.HostRuns[index].Status = event.HostStatus
		}
		m.liveRun.HostRuns[index].ErrorSummary = event.Message
		m.liveRun.HostRuns[index].RebootRequired = event.Reboot
		return
	}
}

func loadRunsCmd(ctx context.Context, client *ipc.Client) tea.Cmd {
	return loadRunsPageCmd(ctx, client, core.RunListQuery{Limit: 100})
}

func loadRunsPageCmd(ctx context.Context, client *ipc.Client, query core.RunListQuery) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		runs, err := client.ListRunsPage(requestCtx, query)
		return runsLoadedMsg{runs: runs, err: err}
	}
}

func loadRunDetailCmd(ctx context.Context, client *ipc.Client, id int64) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		detail, err := client.GetRun(requestCtx, id)
		if err != nil {
			return runDetailLoadedMsg{detail: detail, err: err}
		}
		logs, err := client.GetRunLogs(requestCtx, id)
		return runDetailLoadedMsg{detail: detail, logs: logs.Entries, err: err}
	}
}

func (m model) runListQuery() core.RunListQuery {
	query := core.RunListQuery{Limit: 100, Offset: m.runOffset}
	for status, enabled := range m.runStatusFilters {
		if enabled {
			query.Statuses = append(query.Statuses, status)
		}
	}
	for trigger, enabled := range m.runTriggerFilters {
		if enabled {
			query.Triggers = append(query.Triggers, trigger)
		}
	}
	now := time.Now().UTC()
	switch m.runDateRange {
	case "24h":
		since := now.Add(-24 * time.Hour)
		query.Since = &since
	case "7d":
		since := now.Add(-7 * 24 * time.Hour)
		query.Since = &since
	case "30d":
		since := now.Add(-30 * 24 * time.Hour)
		query.Since = &since
	}
	return query
}

func (m *model) toggleRunStatusFilter(status core.RunStatus) {
	if m.runStatusFilters == nil {
		m.runStatusFilters = map[core.RunStatus]bool{}
	}
	m.runStatusFilters[status] = !m.runStatusFilters[status]
	m.runOffset = 0
}

func (m *model) toggleRunTriggerFilter(trigger core.RunTrigger) {
	if m.runTriggerFilters == nil {
		m.runTriggerFilters = map[core.RunTrigger]bool{}
	}
	m.runTriggerFilters[trigger] = !m.runTriggerFilters[trigger]
	m.runOffset = 0
}

func (m model) runFilterSummary() string {
	rangeLabel := m.runDateRange
	if rangeLabel == "" {
		rangeLabel = "all"
	}
	parts := []string{"range " + rangeLabel, fmt.Sprintf("offset %d", m.runOffset)}
	if len(m.runStatusFilters) > 0 {
		statuses := []string{}
		for status, enabled := range m.runStatusFilters {
			if enabled {
				statuses = append(statuses, string(status))
			}
		}
		if len(statuses) > 0 {
			parts = append(parts, "status "+strings.Join(statuses, ","))
		}
	}
	if len(m.runTriggerFilters) > 0 {
		triggers := []string{}
		for trigger, enabled := range m.runTriggerFilters {
			if enabled {
				triggers = append(triggers, string(trigger))
			}
		}
		if len(triggers) > 0 {
			parts = append(parts, "trigger "+strings.Join(triggers, ","))
		}
	}
	return strings.Join(parts, "  ")
}

func nextRunDateRange(current string) string {
	switch current {
	case "":
		return "24h"
	case "24h":
		return "7d"
	case "7d":
		return "30d"
	default:
		return ""
	}
}

func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	return (time.Duration(seconds) * time.Second).String()
}

func waitRunEventCmd(events <-chan stream.Event, errors <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case event, ok := <-events:
			if !ok {
				return runStreamErrorMsg{}
			}
			return runStreamMsg{event: event}
		case err, ok := <-errors:
			if !ok {
				return runStreamErrorMsg{}
			}
			return runStreamErrorMsg{err: err}
		}
	}
}

func cancelRunCmd(ctx context.Context, client *ipc.Client, id int64) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		err := client.CancelRun(requestCtx, id)
		return runCancelledMsg{err: err}
	}
}
