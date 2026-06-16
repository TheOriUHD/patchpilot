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
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
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

type historyLogViewer struct {
	title  string
	lines  []string
	offset int
}

type autoRebootState struct {
	active          bool
	hostID          int64
	hostName        string
	secondsLeft     int
	queue           []core.HostRun
	gapSecondsLeft  int
	cancelledHostID int64
	rebootingHostID int64
}

func (m model) handleRunsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "m":
		if m.runTreeDetail == nil {
			if _, ok := m.selectedRun(); ok {
				return m.openRunContextMenu(m.runSelected, 4, m.renderBodyY+5+m.runSelected), clearScreenOnTransition()
			}
		}
		return m, nil
	case "enter":
		if m.runTreeDetail != nil {
			return m.openSelectedHistoryHostLog()
		}
		if run, ok := m.selectedRun(); ok {
			return m, loadRunDetailCmd(m.ctx, m.client, run.ID)
		}
		return m, nil
	case "down", "j":
		m = m.clearHover()
		if m.runTreeDetail != nil {
			if m.runTreeHostIndex < len(m.runTreeDetail.HostRuns)-1 {
				m.runTreeHostIndex++
			}
			return m, nil
		}
		if m.runSelected < len(m.runs)-1 {
			m.runSelected++
		}
		return m, nil
	case "up", "k":
		m = m.clearHover()
		if m.runTreeDetail != nil {
			if m.runTreeHostIndex > 0 {
				m.runTreeHostIndex--
			}
			return m, nil
		}
		if m.runSelected > 0 {
			m.runSelected--
		}
		return m, nil
	case "esc", "backspace":
		if m.runTreeDetail != nil {
			m.runTreeDetail = nil
			m.runTreeLogs = nil
			m.runTreeHostIndex = 0
			return m, clearScreenOnTransition()
		}
		return m, nil
	}
	return m, nil
}

func (m model) renderRunsView(bodyHeight int) string {
	if m.runLoading {
		return m.centerCard(bodyHeight, m.styles.Card.Render(m.spinner.View()+" Loading runs"))
	}
	if m.runError != "" {
		return m.centerCard(bodyHeight, m.styles.Card.Width(64).Render(m.styles.ErrorText.Render("Could not load runs")+"\n\n"+m.runError))
	}
	if len(m.runs) == 0 {
		return m.centerCard(bodyHeight, m.styles.Card.Width(58).Render(m.styles.CardTitle.Render("Runs")+"\n\nNo runs have been recorded yet."))
	}
	if m.runTreeDetail != nil {
		return m.renderRunTree(bodyHeight)
	}
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Logs"))
	builder.WriteString("  ")
	builder.WriteString(m.styles.MutedText.Render(m.runFilterSummary()))
	builder.WriteString("\n\n")
	fmt.Fprintf(&builder, "%-2s %-18s %-20s %-7s %-8s %s\n", "", "Started", "Trigger", "Hosts", "Dur", "Status")
	builder.WriteString(m.styles.MutedText.Render(strings.Repeat("─", 78)))
	builder.WriteByte('\n')
	for index, run := range m.runs {
		rowY := m.renderBodyY + 5 + index
		if m.regions != nil {
			m.regions.Add(region{ID: fmt.Sprintf("run-row:%d", index), Rect: Rect{X: 2, Y: rowY, Width: 94, Height: 1}, Action: regionActionRunRow, Index: index})
		}
		cursor := " "
		if index == m.runSelected {
			cursor = theme.SymbolPointer
		}
		trigger := string(run.Trigger)
		if run.Trigger == core.RunTriggerScheduled && run.ScheduleName != "" {
			trigger = run.ScheduleName
		}
		line := fmt.Sprintf("%-2s %-18s %-20s %-7d %-8s %s", cursor, run.StartedAt.Format("Jan 02 15:04"), truncate(trigger, 20), run.HostCount, formatDuration(run.DurationSeconds), runStatusIcon(run.Status)+" "+string(run.Status))
		switch index {
		case m.runSelected:
			builder.WriteString(m.styles.ActiveTab.Render(line))
		case m.hoveredRun:
			builder.WriteString(components.RenderListRow(m.styles, components.Row{Text: line, Hover: true}))
		default:
			builder.WriteString(line)
		}
		builder.WriteByte('\n')
	}
	return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Padding(1, 2).Render(builder.String())
}

func (m model) renderLiveRunOverlay(bodyHeight int) string {
	detail := *m.liveRun
	var builder strings.Builder
	title := m.liveRunTitle()
	builder.WriteString(m.styles.CardTitle.Render(title))
	builder.WriteString(strings.Repeat(" ", max(1, m.modalWidth()-lipgloss.Width(title)-6)))
	if m.liveCompleted {
		builder.WriteString(runStatusIcon(detail.Run.Status))
	} else {
		builder.WriteString(theme.SymbolRefresh)
	}
	if m.liveCancel {
		builder.WriteString("  ")
		builder.WriteString(m.styles.ErrorText.Render("Cancel this run? y/n"))
	}
	builder.WriteByte('\n')
	builder.WriteString(m.styles.MutedText.Render(strings.Repeat("─", max(20, m.modalWidth()-4))))
	builder.WriteByte('\n')
	logHeight := max(6, m.modalHeight(bodyHeight)-10)
	logEntries := m.filteredLiveLogLines(0)
	logLines := make([]string, 0, len(logEntries))
	for _, entry := range logEntries {
		logLines = append(logLines, entry.line)
	}
	visibleLogs := RenderClampedLines(ViewportInput{Lines: logLines, Width: max(20, m.modalWidth()-6), Height: logHeight, ScrollOffset: m.liveScrollOffset})
	for _, line := range visibleLogs {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	builder.WriteString(m.styles.MutedText.Render(strings.Repeat("─", max(20, m.modalWidth()-4))))
	builder.WriteByte('\n')
	if m.liveCompleted {
		builder.WriteString(m.liveCompletionSummary())
	} else {
		builder.WriteString(m.liveProgressFooter())
	}
	builder.WriteString("\n")
	builder.WriteString(m.liveModalKeybinds())
	modal := m.styles.Card.Width(m.modalWidth()).Height(m.modalHeight(bodyHeight)).Render(builder.String())
	return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, modal, lipgloss.WithWhitespaceChars(" "))
}

func (m model) renderRunTree(bodyHeight int) string {
	detail := *m.runTreeDetail
	var builder strings.Builder
	fmt.Fprintf(&builder, "Run #%d · %s · %s · %s · %s %s\n", detail.Run.ID, runTriggerLabel(detail.Run), detail.Run.StartedAt.Format("2006-01-02 15:04"), formatDuration(detail.Run.DurationSeconds), runStatusIcon(detail.Run.Status), detail.Run.Status)
	for index, hostRun := range detail.HostRuns {
		cursor := " "
		if index == m.runTreeHostIndex {
			cursor = theme.SymbolPointer
		}
		reboot := "-"
		if hostRun.RebootRequired {
			reboot = "reboot required"
		}
		prefix := "├"
		if index == len(detail.HostRuns)-1 {
			prefix = "└"
		}
		line := fmt.Sprintf("%s %s %-24s %s %-12s %-7s %-12s %s", cursor, prefix, truncate(hostRun.HostName, 24), hostRunStatusIcon(hostRun.Status), hostRun.Status, hostRunDuration(hostRun), packageCount(hostRun), truncate(firstNonEmpty(hostRun.ErrorSummary, reboot), 32))
		if index == m.runTreeHostIndex {
			builder.WriteString(m.styles.ActiveTab.Render(line))
		} else {
			builder.WriteString(line)
		}
		builder.WriteByte('\n')
	}
	builder.WriteString("\nEnter opens selected host log   Esc returns to list")
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

func (m *model) openLiveRun(detail core.RunDetail) tea.Cmd {
	m.clearConfirmations()
	m.liveRun = &detail
	m.liveLogLines = nil
	m.liveHostIndex = 0
	m.liveModalVisible = true
	m.liveMinimized = false
	m.liveCompleted = terminalRunStatus(detail.Run.Status)
	m.liveScrollOffset = 0
	m.liveStartedAt = time.Now()
	m.liveCancel = false
	m.runBanner = ""
	m.liveAvailableRunID = detail.Run.ID
	m.runTreeDetail = nil
	m.runTreeLogs = nil
	return clearScreenOnTransition()
}

func (m model) handleLiveRunKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.liveCancel {
		switch key.String() {
		case "y", "enter":
			m.liveCancel = false
			if m.liveRun == nil {
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
	if m.liveRun == nil {
		return m, nil
	}
	switch key.String() {
	case "esc":
		if m.liveCompleted {
			m.liveModalVisible = false
			m.liveMinimized = false
			return m, clearScreenOnTransition()
		}
		m.liveModalVisible = false
		m.liveMinimized = true
		m.runBanner = "⟳ Run in progress on " + m.liveRunHostLabel() + " — press L to view"
		return m, clearScreenOnTransition()
	case "K":
		if !m.liveCompleted {
			m.liveCancel = true
			m.status = "Confirm run cancellation"
		}
		return m, nil
	case "pgup":
		m.liveScrollOffset += 10
		return m, nil
	case "pgdown":
		m.liveScrollOffset = max(0, m.liveScrollOffset-10)
		return m, nil
	case "c", "C":
		if m.autoReboot.active {
			m.autoReboot.cancelledHostID = m.autoReboot.hostID
			m.autoReboot.active = false
			m.status = "Auto-reboot cancelled"
		}
		return m, nil
	case "r", "R":
		if hostRun, ok := m.firstRebootReadyHost(); ok {
			m.autoReboot.rebootingHostID = hostRun.HostID
			m.status = "Rebooting " + hostRun.HostName + "... connection will drop momentarily."
			return m, rebootHostCmd(m.ctx, m.client, hostRun.HostID)
		}
		return m, nil
	case "v", "V":
		if m.liveRun != nil {
			return m, loadRunDetailCmd(m.ctx, m.client, m.liveRun.Run.ID)
		}
	case "q", "enter":
		if m.liveCompleted {
			m.liveModalVisible = false
			m.liveMinimized = false
			return m, clearScreenOnTransition()
		}
	}
	return m, nil
}

func (m model) handleHistoryLogViewerKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.logViewer == nil {
		return m, nil
	}
	switch key.String() {
	case "esc", "q":
		m.logViewer = nil
		return m, clearScreenOnTransition()
	case "pgup":
		m.logViewer.offset += 10
	case "pgdown":
		m.logViewer.offset = max(0, m.logViewer.offset-10)
	}
	return m, nil
}

func (m model) renderHistoryLogViewer(bodyHeight int) string {
	viewer := *m.logViewer
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render(viewer.title))
	builder.WriteByte('\n')
	builder.WriteString(m.styles.MutedText.Render(strings.Repeat("─", max(20, m.modalWidth()-4))))
	builder.WriteByte('\n')
	logHeight := max(6, m.modalHeight(bodyHeight)-5)
	start := max(0, len(viewer.lines)-logHeight-viewer.offset)
	end := min(len(viewer.lines), start+logHeight)
	for _, line := range viewer.lines[start:end] {
		builder.WriteString(truncate(line, max(20, m.modalWidth()-6)))
		builder.WriteByte('\n')
	}
	builder.WriteString("Esc close  PgUp/PgDn scroll")
	modal := m.styles.Card.Width(m.modalWidth()).Height(m.modalHeight(bodyHeight)).Render(builder.String())
	return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, modal)
}

func (m model) openSelectedHistoryHostLog() (tea.Model, tea.Cmd) {
	if m.runTreeDetail == nil || len(m.runTreeDetail.HostRuns) == 0 {
		return m, nil
	}
	hostRun := m.runTreeDetail.HostRuns[m.runTreeHostIndex]
	lines := []string{}
	for _, entry := range m.runTreeLogs {
		if entry.HostRunID == hostRun.ID {
			lines = append(lines, entry.Line)
		}
	}
	m.logViewer = &historyLogViewer{title: "Run log: " + hostRun.HostName, lines: lines}
	return m, clearScreenOnTransition()
}

func (m *model) observeActiveRun() {
	for _, run := range m.runs {
		if run.Status != core.RunStatusRunning && run.Status != core.RunStatusQueued {
			continue
		}
		if m.liveRun != nil && m.liveRun.Run.ID == run.ID {
			return
		}
		m.liveAvailableRunID = run.ID
		label := "Run in progress on " + fmt.Sprintf("%d hosts", run.HostCount)
		if run.Trigger == core.RunTriggerScheduled {
			label = fmt.Sprintf("Scheduled run started on %d hosts", run.HostCount)
		}
		m.runBanner = theme.SymbolRefresh + " " + label + " — press L to view live"
		return
	}
	if m.liveRun == nil || terminalRunStatus(m.liveRun.Run.Status) {
		m.liveAvailableRunID = 0
		if !m.liveMinimized {
			m.runBanner = ""
		}
	}
}

func (m model) renderRunBanner() string {
	if m.runBanner == "" {
		return ""
	}
	return m.styles.StatusBar.Width(m.width).Render(m.runBanner)
}

func (m model) modalWidth() int {
	return max(60, min(m.width-4, m.width*8/10))
}

func (m model) modalHeight(bodyHeight int) int {
	return max(18, min(bodyHeight, bodyHeight*7/10))
}

func (m model) liveRunTitle() string {
	verb := "Updating"
	if m.liveCompleted {
		verb = "Updated"
	}
	if m.liveRun == nil || len(m.liveRun.HostRuns) != 1 {
		count := 0
		if m.liveRun != nil {
			count = len(m.liveRun.HostRuns)
		}
		return fmt.Sprintf("%s %d hosts", verb, count)
	}
	return verb + " " + m.liveRun.HostRuns[0].HostName
}

func (m model) liveRunHostLabel() string {
	if m.liveRun == nil || len(m.liveRun.HostRuns) == 0 {
		return "hosts"
	}
	if len(m.liveRun.HostRuns) == 1 {
		return m.liveRun.HostRuns[0].HostName
	}
	return fmt.Sprintf("%d hosts", len(m.liveRun.HostRuns))
}

func (m model) liveProgressFooter() string {
	current := 0
	summary := "waiting"
	if m.liveRun != nil {
		for index, hostRun := range m.liveRun.HostRuns {
			if hostRun.Status == core.HostRunStatusRunning {
				current = index + 1
				summary = firstNonEmpty(hostRun.ErrorSummary, "updating "+hostRun.HostName)
				break
			}
		}
		if current == 0 {
			current = min(len(m.liveRun.HostRuns), 1)
		}
	}
	return fmt.Sprintf("Host %d of %d · elapsed %s · %s", current, len(m.liveRun.HostRuns), time.Since(m.liveStartedAt).Round(time.Second), summary)
}

func (m model) liveCompletionSummary() string {
	if m.liveRun == nil {
		return ""
	}
	success, failed, reboot := 0, 0, 0
	for _, hostRun := range m.liveRun.HostRuns {
		if hostRun.Status == core.HostRunStatusSuccess {
			success++
		}
		if hostRun.Status == core.HostRunStatusFailed {
			failed++
		}
		if hostRun.RebootRequired {
			reboot++
		}
	}
	if len(m.liveRun.HostRuns) == 1 {
		line := fmt.Sprintf("%s Completed in %s", runStatusIcon(m.liveRun.Run.Status), formatDuration(m.liveRun.Run.DurationSeconds))
		if reboot > 0 {
			line += " · ⚠ Reboot required"
			if m.autoReboot.active {
				line += fmt.Sprintf(" · Auto-reboot in %d... [ Cancel ]", m.autoReboot.secondsLeft)
			}
		} else {
			line += " · All up to date · no reboot needed"
		}
		return line
	}
	return fmt.Sprintf("Completed in %s · %d success · %d failed · %d need reboot", formatDuration(m.liveRun.Run.DurationSeconds), success, failed, reboot)
}

func (m model) liveModalKeybinds() string {
	if m.liveCompleted {
		if m.autoReboot.active {
			return "C cancel auto-reboot  V view full log  Enter close"
		}
		if _, ok := m.firstRebootReadyHost(); ok {
			return "R reboot now  V view full log  Enter close"
		}
		return "V view full log  Enter close"
	}
	return "K cancel  PgUp/PgDn scroll  Esc minimize"
}

func (m *model) prepareAutoRebootQueue() tea.Cmd {
	m.autoReboot = autoRebootState{}
	if m.liveRun == nil || m.liveRun.Run.Status != core.RunStatusSuccess {
		return nil
	}
	for _, hostRun := range m.liveRun.HostRuns {
		if !hostRun.RebootRequired || hostRun.Status != core.HostRunStatusSuccess {
			continue
		}
		host, ok := m.hostByID(hostRun.HostID)
		if ok && host.AutoReboot {
			hostRun.ErrorSummary = fmt.Sprintf("countdown %d", host.AutoRebootDelay)
			m.autoReboot.queue = append(m.autoReboot.queue, hostRun)
		}
	}
	return m.startNextAutoRebootCountdown()
}

func (m *model) startNextAutoRebootCountdown() tea.Cmd {
	if len(m.autoReboot.queue) == 0 {
		return nil
	}
	hostRun := m.autoReboot.queue[0]
	m.autoReboot.queue = m.autoReboot.queue[1:]
	host, _ := m.hostByID(hostRun.HostID)
	m.autoReboot.active = true
	m.autoReboot.hostID = hostRun.HostID
	m.autoReboot.hostName = hostRun.HostName
	m.autoReboot.secondsLeft = core.NormalizeRebootDelay(host.AutoRebootDelay)
	return autoRebootTickCmd()
}

func (m model) handleAutoRebootTick() (tea.Model, tea.Cmd) {
	if m.autoReboot.gapSecondsLeft > 0 {
		m.autoReboot.gapSecondsLeft--
		if m.autoReboot.gapSecondsLeft > 0 {
			return m, autoRebootTickCmd()
		}
		return m, m.startNextAutoRebootCountdown()
	}
	if !m.autoReboot.active {
		return m, nil
	}
	m.autoReboot.secondsLeft--
	if m.autoReboot.secondsLeft > 0 {
		return m, autoRebootTickCmd()
	}
	hostID := m.autoReboot.hostID
	m.autoReboot.active = false
	m.autoReboot.rebootingHostID = hostID
	m.status = "Rebooting " + m.autoReboot.hostName + "... connection will drop momentarily."
	return m, rebootHostCmd(m.ctx, m.client, hostID)
}

func (m model) handleHostRebootMsg(msg hostRebootMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = msg.err.Error()
		return m, nil
	}
	m.status = "Reboot command sent"
	if len(m.autoReboot.queue) > 0 {
		m.autoReboot.gapSecondsLeft = 10
		return m, autoRebootTickCmd()
	}
	return m, nil
}

func (m model) firstRebootReadyHost() (core.HostRun, bool) {
	if m.liveRun == nil || m.liveRun.Run.Status != core.RunStatusSuccess {
		return core.HostRun{}, false
	}
	for _, hostRun := range m.liveRun.HostRuns {
		if hostRun.Status == core.HostRunStatusSuccess && hostRun.RebootRequired && hostRun.HostID != m.autoReboot.cancelledHostID && hostRun.HostID != m.autoReboot.rebootingHostID {
			return hostRun, true
		}
	}
	return core.HostRun{}, false
}

func (m model) hostByID(id int64) (core.Host, bool) {
	for _, host := range m.hosts {
		if host.ID == id {
			return host, true
		}
	}
	return core.Host{}, false
}

func terminalRunStatus(status core.RunStatus) bool {
	switch status {
	case core.RunStatusSuccess, core.RunStatusPartialFailure, core.RunStatusFailed, core.RunStatusCancelled:
		return true
	default:
		return false
	}
}

func runStatusIcon(status core.RunStatus) string {
	switch status {
	case core.RunStatusSuccess:
		return theme.SymbolOK
	case core.RunStatusPartialFailure:
		return theme.SymbolWarning
	case core.RunStatusFailed:
		return theme.SymbolError
	case core.RunStatusCancelled:
		return theme.SymbolDisabled
	case core.RunStatusRunning, core.RunStatusQueued:
		return theme.SymbolRefresh
	default:
		return "-"
	}
}

func hostRunStatusIcon(status core.HostRunStatus) string {
	switch status {
	case core.HostRunStatusSuccess:
		return theme.SymbolOK
	case core.HostRunStatusFailed:
		return theme.SymbolError
	case core.HostRunStatusSkipped:
		return theme.SymbolDisabled
	case core.HostRunStatusRunning, core.HostRunStatusPending:
		return theme.SymbolRefresh
	default:
		return "-"
	}
}

func runTriggerLabel(run core.Run) string {
	if run.Trigger == core.RunTriggerScheduled && run.ScheduleName != "" {
		return fmt.Sprintf("scheduled %q", run.ScheduleName)
	}
	return string(run.Trigger)
}

func hostRunDuration(hostRun core.HostRun) string {
	if hostRun.StartedAt == nil || hostRun.FinishedAt == nil {
		return "-"
	}
	return hostRun.FinishedAt.Sub(*hostRun.StartedAt).Round(time.Second).String()
}

func packageCount(hostRun core.HostRun) string {
	if hostRun.PackagesUpgradedCount == 0 {
		return "-"
	}
	return fmt.Sprintf("%d packages", hostRun.PackagesUpgradedCount)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "-"
}

func (m model) runKeybinds() []string {
	if m.liveCancel {
		return []string{"y cancel", "n keep running"}
	}
	if m.liveRun != nil {
		if m.liveModalVisible {
			return []string{"K cancel", "PgUp/PgDn scroll", "Esc minimize"}
		}
		return []string{"L live run"}
	}
	if m.runTreeDetail != nil {
		return []string{"↑/↓ host", "Enter log", "Esc list"}
	}
	return []string{"↑/↓ navigate", "Enter open", "f range", "S/F status", "M/A/C trigger", "m menu", "o older"}
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
			if cmd := m.applyRunEvent(runEvent); cmd != nil {
				return tea.Batch(cmd, waitRunEventCmd(m.runEvents, m.runEventErrors))
			}
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

func (m *model) applyRunEvent(event core.RunEvent) tea.Cmd {
	if m.liveRun == nil {
		return nil
	}
	var cmd tea.Cmd
	if event.RunStatus != "" {
		m.liveRun.Run.Status = event.RunStatus
		m.liveRun.Run.Summary = event.Message
		m.status = event.Message
		if terminalRunStatus(event.RunStatus) && !m.liveCompleted {
			m.liveCompleted = true
			if m.liveRun.Run.DurationSeconds == 0 && !m.liveStartedAt.IsZero() {
				m.liveRun.Run.DurationSeconds = int64(time.Since(m.liveStartedAt).Seconds())
			}
			m.liveLogLines = append(m.liveLogLines, liveLogEntry{line: "FINISHED"})
			cmd = m.prepareAutoRebootQueue()
		}
	}
	if event.HostRunID == 0 {
		return cmd
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
		return cmd
	}
	return cmd
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

func runPollCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return runPollTickMsg{} })
}

func autoRebootTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return autoRebootTickMsg{} })
}

func rebootHostCmd(ctx context.Context, client *ipc.Client, id int64) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		err := client.RebootHost(requestCtx, id)
		return hostRebootMsg{hostID: id, err: err}
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
