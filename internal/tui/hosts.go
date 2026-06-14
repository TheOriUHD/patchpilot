package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/ipc"
	"github.com/TheOriUHD/patchpilot/internal/pkgmgr"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

type hostScreen int

const (
	hostScreenList hostScreen = iota
	hostScreenDetail
	hostScreenWizard
)

type pendingAction struct {
	Kind string
	Host core.Host
}

type hostsLoadedMsg struct {
	hosts []core.Host
	err   error
}

type hostMutationMsg struct {
	message string
	err     error
}

type dryRunLoadedMsg struct {
	result pkgmgr.DryRunResult
	err    error
}

func (m model) handleHostsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.hostScreen == hostScreenWizard {
		return m.handleWizardKey(key)
	}
	if m.pending.Kind != "" {
		switch key.String() {
		case "y", "enter":
			return m.confirmPending()
		case "n", "esc":
			m.pending = pendingAction{}
			m.status = "Cancelled"
			return m, nil
		}
		return m, nil
	}
	if m.filtering {
		return m.handleFilterKey(key)
	}
	if m.renameMode {
		return m.handleRenameKey(key)
	}

	switch m.hostScreen {
	case hostScreenDetail:
		return m.handleDetailKey(key)
	default:
		return m.handleHostListKey(key)
	}
}

func (m model) handleHostListKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "a":
		m.hostScreen = hostScreenWizard
		m.wizard = newStandardWizard()
		m.status = "Add host"
		return m, nil
	case "down", "j":
		if m.hostSelected < len(m.filteredHosts())-1 {
			m.hostSelected++
		}
		return m, nil
	case "up", "k":
		if m.hostSelected > 0 {
			m.hostSelected--
		}
		return m, nil
	case "enter":
		if host, ok := m.selectedHost(); ok {
			m.hostScreen = hostScreenDetail
			m.renameBuffer = host.Name
		}
		return m, nil
	case "e":
		if host, ok := m.selectedHost(); ok {
			if m.hostInProgress(host.ID) {
				m.status = "cannot toggle during update"
				return m, nil
			}
			m.pending = pendingAction{Kind: "toggle", Host: host}
		}
		return m, nil
	case "R":
		if host, ok := m.selectedHost(); ok {
			if m.hostInProgress(host.ID) {
				m.status = "host is already being updated"
				return m, nil
			}
			m.pending = pendingAction{Kind: "run", Host: host}
		}
		return m, nil
	case "/":
		m.filtering = true
		return m, nil
	}
	return m, nil
}

func (m model) handleDetailKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "esc":
		m.hostScreen = hostScreenList
		m.detailTab = 0
		return m, nil
	case "tab":
		m.detailTab = (m.detailTab + 1) % 3
		return m, nil
	case "shift+tab":
		m.detailTab = (m.detailTab + 2) % 3
		return m, nil
	case "u":
		if host, ok := m.selectedHost(); ok {
			if m.hostInProgress(host.ID) {
				m.status = "host is already being updated"
				return m, nil
			}
			m.pending = pendingAction{Kind: "run", Host: host}
		}
		return m, nil
	case "d":
		if host, ok := m.selectedHost(); ok {
			m.status = "Dry run started"
			return m, dryRunHostCmd(m.ctx, m.client, host.ID)
		}
		return m, nil
	case "n":
		if host, ok := m.selectedHost(); ok {
			if m.hostInProgress(host.ID) {
				m.status = "cannot rename during update"
				return m, nil
			}
			m.renameMode = true
			m.renameBuffer = host.Name
		}
		return m, nil
	case "e":
		if host, ok := m.selectedHost(); ok {
			if m.hostInProgress(host.ID) {
				m.status = "cannot toggle during update"
				return m, nil
			}
			m.pending = pendingAction{Kind: "toggle", Host: host}
		}
		return m, nil
	case "b":
		if host, ok := m.selectedHost(); ok {
			if m.hostInProgress(host.ID) {
				m.status = "cannot edit reboot policy during update"
				return m, nil
			}
			input := hostUpdateFromHost(host)
			input.AutoReboot = !host.AutoReboot
			input.AutoRebootDelay = core.NormalizeRebootDelay(host.AutoRebootDelay)
			message := "Reboot policy set to report only"
			if input.AutoReboot {
				message = "Reboot policy set to auto-reboot"
			}
			return m, updateHostCmd(m.ctx, m.client, host.ID, input, message)
		}
		return m, nil
	case "P":
		if host, ok := m.selectedHost(); ok {
			if m.hostInProgress(host.ID) {
				m.status = "cannot repair sudoers during update"
				return m, nil
			}
			m.pending = pendingAction{Kind: "repair-sudoers", Host: host}
		}
		return m, nil
	case "x":
		if host, ok := m.selectedHost(); ok {
			if m.hostInProgress(host.ID) {
				m.status = "cannot remove host while update is in progress"
				return m, nil
			}
			m.pending = pendingAction{Kind: "remove", Host: host}
		}
		return m, nil
	}
	return m, nil
}

func (m model) handleFilterKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.filtering = false
		return m, nil
	case "enter":
		m.filtering = false
		m.hostSelected = 0
		return m, nil
	case "backspace":
		if len(m.hostFilter) > 0 {
			m.hostFilter = m.hostFilter[:len(m.hostFilter)-1]
		}
		return m, nil
	}
	if len(key.Runes) > 0 {
		m.hostFilter += string(key.Runes)
	}
	return m, nil
}

func (m model) handleRenameKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.renameMode = false
		return m, nil
	case "enter":
		host, ok := m.selectedHost()
		if !ok {
			m.renameMode = false
			return m, nil
		}
		m.renameMode = false
		input := hostUpdateFromHost(host)
		input.Name = strings.TrimSpace(m.renameBuffer)
		return m, updateHostCmd(m.ctx, m.client, host.ID, input, "Host renamed")
	case "backspace":
		if len(m.renameBuffer) > 0 {
			m.renameBuffer = m.renameBuffer[:len(m.renameBuffer)-1]
		}
		return m, nil
	}
	if len(key.Runes) > 0 {
		m.renameBuffer += string(key.Runes)
	}
	return m, nil
}

func (m model) confirmPending() (tea.Model, tea.Cmd) {
	action := m.pending
	m.pending = pendingAction{}
	switch action.Kind {
	case "toggle":
		return m, toggleHostCmd(m.ctx, m.client, action.Host.ID)
	case "remove":
		m.hostScreen = hostScreenList
		return m, deleteHostCmd(m.ctx, m.client, action.Host.ID)
	case "run":
		return m, runHostCmd(m.ctx, m.client, action.Host.ID)
	case "repair-sudoers":
		return m, repairSudoersCmd(m.ctx, m.client, action.Host.ID, action.Host.Name)
	}
	return m, nil
}

func (m model) renderHostsView(bodyHeight int) string {
	if m.hostScreen == hostScreenWizard {
		return m.renderWizard(bodyHeight)
	}
	content := m.renderHostContent(bodyHeight)
	if m.pending.Kind != "" {
		content = lipgloss.JoinVertical(lipgloss.Center, content, m.renderConfirm())
	}
	return content
}

func (m model) renderHostContent(bodyHeight int) string {
	if m.hostLoading {
		return m.centerCard(bodyHeight, m.styles.Card.Render(m.spinner.View()+" Loading hosts"))
	}
	if m.hostError != "" {
		return m.centerCard(bodyHeight, m.styles.Card.Width(64).Render(m.styles.ErrorText.Render("Could not load hosts")+"\n\n"+m.hostError))
	}
	if m.hostScreen == hostScreenDetail {
		return m.renderHostDetail(bodyHeight)
	}
	return m.renderHostList(bodyHeight)
}

func (m model) renderHostList(bodyHeight int) string {
	hosts := m.filteredHosts()
	if len(hosts) == 0 {
		message := "No hosts yet - press a to add your first host."
		if m.hostFilter != "" {
			message = "No hosts match filter: " + m.hostFilter
		}
		return m.centerCard(bodyHeight, m.styles.Card.Width(58).Render(m.styles.CardTitle.Render("Hosts")+"\n\n"+message))
	}

	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Hosts"))
	if m.filtering || m.hostFilter != "" {
		builder.WriteString("  ")
		builder.WriteString(m.styles.MutedText.Render("filter: " + m.hostFilter))
	}
	builder.WriteString("\n\n")
	fmt.Fprintf(&builder, "%-2s %-22s %-12s %-12s %-12s %-8s %s\n", "", "Name", "Type", "Package", "Last", "Updates", "Reboot")
	builder.WriteString(m.styles.MutedText.Render(strings.Repeat("─", 78)))
	builder.WriteByte('\n')
	for index, host := range hosts {
		cursor := " "
		if index == m.hostSelected {
			cursor = theme.SymbolPointer
		}
		status := theme.SymbolOK
		if !host.Enabled {
			status = theme.SymbolDisabled
		}
		if m.hostInProgress(host.ID) {
			status = theme.SymbolRefresh
		}
		line := fmt.Sprintf("%-2s %-22s %-12s %-12s %-12s %-8s %s", cursor+status, truncate(host.Name, 22), host.Type, host.PackageManager, "-", "-", m.hostRebootHint(host.ID))
		if m.hostInProgress(host.ID) {
			line = m.styles.MutedText.Render(line)
		}
		if index == m.hostSelected {
			builder.WriteString(m.styles.ActiveTab.Render(line))
		} else {
			builder.WriteString(line)
		}
		builder.WriteByte('\n')
	}
	return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Padding(1, 2).Render(builder.String())
}

func (m model) renderHostDetail(bodyHeight int) string {
	host, ok := m.selectedHost()
	if !ok {
		return m.centerCard(bodyHeight, m.styles.Card.Render("No host selected"))
	}
	tabs := []string{"Overview", "History", "Logs"}
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render(host.Name))
	builder.WriteString("  ")
	for index, tab := range tabs {
		if index == m.detailTab {
			builder.WriteString(m.styles.ActiveTab.Render("[ " + tab + " ]"))
		} else {
			builder.WriteString(m.styles.Tab.Render("[ " + tab + " ]"))
		}
	}
	builder.WriteString("\n\n")
	if m.renameMode {
		builder.WriteString("Rename: ")
		builder.WriteString(m.styles.ActiveTab.Render(m.renameBuffer))
		builder.WriteString("\n\n")
	}
	switch m.detailTab {
	case 0:
		if host.Type == core.HostTypeTrueNAS || host.Type == core.HostTypeHAOS || host.PackageManager == string(pkgmgr.ManagerTrueNASAPI) || host.PackageManager == string(pkgmgr.ManagerHAOSAPI) {
			fmt.Fprintf(&builder, "Endpoint: %s\n", host.Hostname)
		} else {
			fmt.Fprintf(&builder, "Hostname: %s:%d\n", host.Hostname, host.SSHPort)
		}
		fmt.Fprintf(&builder, "Type: %s\nPackage: %s\nOS: %s\nArch: %s\nEnabled: %v\nReboot: %s\nReboot policy: %s\nNotes: %s\n", host.Type, host.PackageManager, host.OSPretty, host.Arch, host.Enabled, m.hostRebootHint(host.ID), rebootPolicyText(host), host.Notes)
		if m.hostInProgress(host.ID) {
			builder.WriteString("\nUpdate in progress: mutating actions disabled. L view live run, K cancel.")
		} else {
			builder.WriteString("\nActions: u update  d dry run  n rename  b reboot policy  P repair sudoers  e enable/disable  x remove  q back")
		}
	case 1:
		builder.WriteString("No host run history yet. Manual runs created here will appear in the Logs milestone view.")
	case 2:
		builder.WriteString("No log selected. Run logs are written by the daemon under its run log directory.")
	}
	return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Padding(1, 2).Render(builder.String())
}

func (m model) renderConfirm() string {
	message := "Confirm?"
	switch m.pending.Kind {
	case "toggle":
		verb := "disable"
		if !m.pending.Host.Enabled {
			verb = "enable"
		}
		message = fmt.Sprintf("%s %s? y/n", titleWord(verb), m.pending.Host.Name)
	case "remove":
		message = fmt.Sprintf("Remove %s from patchpilot? The patchpilot user on the host will be left in place. y/n", m.pending.Host.Name)
	case "run":
		message = fmt.Sprintf("Run updates now on %s? y/n", m.pending.Host.Name)
	case "repair-sudoers":
		message = fmt.Sprintf("Repair sudoers entry on %s? This will update the NOPASSWD whitelist to include reboot commands. y/n", m.pending.Host.Name)
	}
	return m.styles.Card.Width(72).Render(message)
}

func (m model) hostKeybinds() []string {
	if m.hostScreen == hostScreenWizard {
		return []string{"Esc cancel", "Tab/↑↓ focus", "Enter continue", "q quit"}
	}
	if m.pending.Kind != "" {
		return []string{"y confirm", "n cancel"}
	}
	if m.hostScreen == hostScreenDetail {
		return []string{"Tab detail tab", "u update", "d dry run", "b reboot", "P repair sudoers", "n rename", "e toggle", "x remove", "q back"}
	}
	return []string{"↑/↓ navigate", "Enter detail", "a add", "R run host", "e toggle", "/ filter", "q quit"}
}

func (m model) filteredHosts() []core.Host {
	if m.hostFilter == "" {
		return m.hosts
	}
	filter := strings.ToLower(m.hostFilter)
	var filtered []core.Host
	for _, host := range m.hosts {
		if strings.Contains(strings.ToLower(host.Name), filter) || strings.Contains(strings.ToLower(string(host.Type)), filter) || strings.Contains(strings.ToLower(host.PackageManager), filter) {
			filtered = append(filtered, host)
		}
	}
	return filtered
}

func (m model) selectedHost() (core.Host, bool) {
	hosts := m.filteredHosts()
	if m.hostSelected < 0 || m.hostSelected >= len(hosts) {
		return core.Host{}, false
	}
	return hosts[m.hostSelected], true
}

func (m model) hostRebootHint(hostID int64) string {
	if m.liveRun == nil {
		return "-"
	}
	for _, hostRun := range m.liveRun.HostRuns {
		if hostRun.HostID == hostID && hostRun.RebootRequired {
			return "yes"
		}
	}
	return "-"
}

func (m model) hostInProgress(hostID int64) bool {
	if m.liveRun == nil {
		return false
	}
	for _, hostRun := range m.liveRun.HostRuns {
		if hostRun.HostID == hostID && (hostRun.Status == core.HostRunStatusPending || hostRun.Status == core.HostRunStatusRunning) {
			return true
		}
	}
	return false
}

func hostUpdateFromHost(host core.Host) core.HostUpdate {
	return core.HostUpdate{Name: host.Name, Enabled: host.Enabled, AutoReboot: host.AutoReboot, AutoRebootDelay: core.NormalizeRebootDelay(host.AutoRebootDelay), Notes: host.Notes}
}

func rebootPolicyText(host core.Host) string {
	if host.AutoReboot {
		return fmt.Sprintf("auto-reboot after successful update (%ds countdown)", core.NormalizeRebootDelay(host.AutoRebootDelay))
	}
	return "report only"
}

func updateHostCmd(ctx context.Context, client *ipc.Client, id int64, input core.HostUpdate, message string) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err := client.UpdateHost(requestCtx, id, input)
		return hostMutationMsg{message: message, err: err}
	}
}

func toggleHostCmd(ctx context.Context, client *ipc.Client, id int64) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err := client.ToggleHost(requestCtx, id)
		return hostMutationMsg{message: "Host updated", err: err}
	}
}

func deleteHostCmd(ctx context.Context, client *ipc.Client, id int64) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		err := client.DeleteHost(requestCtx, id)
		return hostMutationMsg{message: "Host removed", err: err}
	}
}

func dryRunHostCmd(ctx context.Context, client *ipc.Client, id int64) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		result, err := client.DryRunHost(requestCtx, id)
		return dryRunLoadedMsg{result: result, err: err}
	}
}

func runHostCmd(ctx context.Context, client *ipc.Client, id int64) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		detail, err := client.RunHostNowDetail(requestCtx, id)
		return runStartedMsg{detail: detail, err: err}
	}
}

func repairSudoersCmd(ctx context.Context, client *ipc.Client, id int64, name string) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		err := client.RepairHostSudoers(requestCtx, id)
		return hostMutationMsg{message: "Sudoers repaired on " + name, err: err}
	}
}

func truncate(value string, width int) string {
	if len(value) <= width {
		return value
	}
	if width <= 1 {
		return value[:width]
	}
	return value[:width-1] + "."
}

func titleWord(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
