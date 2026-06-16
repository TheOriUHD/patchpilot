package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/ipc"
	"github.com/TheOriUHD/patchpilot/internal/stream"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

type wizardStep int

const (
	wizardStepInput wizardStep = iota
	wizardStepDetecting
	wizardStepDetected
	wizardStepDiagnose
	wizardStepPreinstall
	wizardStepBootstrapping
	wizardStepReview
	wizardStepError
)

type standardWizard struct {
	step         wizardStep
	focus        int
	hostType     int
	hostname     string
	sshUser      string
	password     []byte
	rootPassword []byte
	name         string
	notes        string
	enabled      bool
	autoReboot   bool
	rebootDelay  int
	facts        core.HostFacts
	diagnosis    *core.PermissionDiagnosis
	refusal      string
	errorMessage string
	apiResult    *core.APIHostVerifyResult
	apiCheck     *core.APIHostVerifyResult
	sessionID    string
	events       []core.BootstrapEvent
	summary      *core.BootstrapSummary
	streamEvents <-chan stream.Event
	streamErrors <-chan error
}

type wizardMsg struct {
	kind         string
	detectResult core.StandardDetectResult
	apiResult    core.APIHostVerifyResult
	session      core.BootstrapSession
	host         core.Host
	events       <-chan stream.Event
	errors       <-chan error
	err          error
}

type bootstrapEventMsg struct {
	event stream.Event
	err   error
	done  bool
}

func newStandardWizard() standardWizard {
	return standardWizard{step: wizardStepInput, enabled: true, rebootDelay: 30}
}

func (m model) handleWizardKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" || key.String() == "q" {
		m.wizard.zeroSecrets()
		if m.wizard.sessionID != "" {
			return m, tea.Batch(cancelBootstrapCmd(m.ctx, m.client, m.wizard.sessionID), tea.Quit)
		}
		return m, tea.Quit
	}
	if key.String() == "esc" || key.String() == "ctrl+[" {
		sessionID := m.wizard.sessionID
		m.wizard.zeroSecrets()
		m.hostScreen = hostScreenList
		m.status = "Add host cancelled; secrets wiped"
		if sessionID != "" {
			return m, withClearScreen(cancelBootstrapCmd(m.ctx, m.client, sessionID), loadHostsCmd(m.ctx, m.client))
		}
		return m, withClearScreen(loadHostsCmd(m.ctx, m.client))
	}

	switch m.wizard.step {
	case wizardStepInput:
		return m.handleWizardInputKey(key)
	case wizardStepDetected:
		if key.String() == "enter" {
			if m.wizard.isAPI() {
				m.wizard.step = wizardStepBootstrapping
				return m, checkAPIHostCmd(m.ctx, m.client, m.wizard)
			}
			m.wizard.step = wizardStepBootstrapping
			m.wizard.events = nil
			return m, startBootstrapCmd(m.ctx, m.client, m.wizard)
		}
	case wizardStepDiagnose:
		return m.handleWizardDiagnoseKey(key)
	case wizardStepPreinstall:
		if key.String() == "r" {
			m.wizard.step = wizardStepDetecting
			return m, detectStandardCmd(m.ctx, m.client, m.wizard)
		}
	case wizardStepReview:
		return m.handleWizardReviewKey(key)
	case wizardStepError:
		if key.String() == "r" {
			m.wizard = newStandardWizard()
			return m, nil
		}
	}
	return m, nil
}

func (m model) handleWizardInputKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	focusCount := m.wizard.inputFocusCount()
	switch key.String() {
	case "tab", "down":
		m.wizard.focus = (m.wizard.focus + 1) % focusCount
		return m, nil
	case "shift+tab", "up":
		m.wizard.focus = (m.wizard.focus + focusCount - 1) % focusCount
		return m, nil
	case "left":
		if m.wizard.focus == 0 && m.wizard.hostType > 0 {
			m.wizard.hostType--
		}
		return m, nil
	case "right":
		if m.wizard.focus == 0 && m.wizard.hostType < 2 {
			m.wizard.hostType++
		}
		return m, nil
	case "backspace":
		m.wizard.backspaceFocused()
		return m, nil
	case "enter":
		if m.wizard.isAPI() {
			if strings.TrimSpace(m.wizard.hostname) == "" || len(m.wizard.password) == 0 {
				m.status = "URL and API token are required"
				return m, nil
			}
			m.wizard.step = wizardStepDetecting
			return m, verifyAPIHostCmd(m.ctx, m.client, m.wizard)
		}
		if strings.TrimSpace(m.wizard.hostname) == "" || strings.TrimSpace(m.wizard.sshUser) == "" || len(m.wizard.password) == 0 {
			m.status = "Hostname, SSH user, and password are required"
			return m, nil
		}
		m.wizard.step = wizardStepDetecting
		return m, detectStandardCmd(m.ctx, m.client, m.wizard)
	}
	if len(key.Runes) > 0 {
		m.wizard.appendFocused(string(key.Runes))
	}
	return m, nil
}

func (m model) handleWizardDiagnoseKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.wizard.isAPI() {
		switch key.String() {
		case "r":
			m.wizard.step = wizardStepDetecting
			return m, verifyAPIHostCmd(m.ctx, m.client, m.wizard)
		case "e":
			m.wizard.step = wizardStepInput
			return m, nil
		}
		return m, nil
	}
	switch key.String() {
	case "tab", "down", "up", "shift+tab":
		return m, nil
	case "backspace":
		if len(m.wizard.rootPassword) > 0 {
			m.wizard.rootPassword = m.wizard.rootPassword[:len(m.wizard.rootPassword)-1]
		}
		return m, nil
	case "f":
		if len(m.wizard.rootPassword) == 0 {
			m.status = "Root password is required for automatic fix"
			return m, nil
		}
		return m, rootAssistCmd(m.ctx, m.client, m.wizard)
	case "p":
		m.wizard.step = wizardStepPreinstall
		return m, nil
	}
	if len(key.Runes) > 0 {
		m.wizard.rootPassword = append(m.wizard.rootPassword, []byte(string(key.Runes))...)
	}
	return m, nil
}

func (m model) handleWizardReviewKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "tab", "down":
		m.wizard.focus = (m.wizard.focus + 1) % 5
		return m, nil
	case "shift+tab", "up":
		m.wizard.focus = (m.wizard.focus + 4) % 5
		return m, nil
	case "backspace":
		if m.wizard.focus == 0 && len(m.wizard.name) > 0 {
			m.wizard.name = m.wizard.name[:len(m.wizard.name)-1]
		}
		if m.wizard.focus == 1 && len(m.wizard.notes) > 0 {
			m.wizard.notes = m.wizard.notes[:len(m.wizard.notes)-1]
		}
		return m, nil
	case " ":
		if m.wizard.focus == 2 {
			m.wizard.enabled = !m.wizard.enabled
		}
		if m.wizard.focus == 3 {
			m.wizard.autoReboot = !m.wizard.autoReboot
		}
		return m, nil
	case "left", "h":
		if m.wizard.focus == 4 {
			m.wizard.rebootDelay = core.NormalizeRebootDelay(m.wizard.rebootDelay - 5)
		}
		return m, nil
	case "right", "l":
		if m.wizard.focus == 4 {
			m.wizard.rebootDelay = core.NormalizeRebootDelay(m.wizard.rebootDelay + 5)
		}
		return m, nil
	case "enter":
		if m.wizard.isAPI() {
			return m, commitAPIHostCmd(m.ctx, m.client, m.wizard)
		}
		return m, commitBootstrapCmd(m.ctx, m.client, m.wizard)
	}
	if len(key.Runes) > 0 {
		if m.wizard.focus == 0 {
			m.wizard.name += string(key.Runes)
		}
		if m.wizard.focus == 1 {
			m.wizard.notes += string(key.Runes)
		}
	}
	return m, nil
}

func (m model) handleWizardMsg(msg wizardMsg) (model, tea.Cmd) {
	switch msg.kind {
	case "detect":
		if msg.err != nil {
			m.wizard.zeroSecrets()
			m.wizard.step = wizardStepError
			m.wizard.errorMessage = msg.err.Error()
			return m, nil
		}
		m.wizard.facts = msg.detectResult.Facts
		m.wizard.diagnosis = msg.detectResult.Diagnosis
		m.wizard.refusal = msg.detectResult.Refusal
		if msg.detectResult.Refusal != "" {
			m.wizard.zeroSecrets()
			m.wizard.step = wizardStepError
			m.wizard.errorMessage = msg.detectResult.Refusal
			return m, nil
		}
		if msg.detectResult.Diagnosis != nil {
			m.wizard.step = wizardStepDiagnose
			return m, nil
		}
		m.wizard.step = wizardStepDetected
		m.wizard.name = fallbackName(m.wizard.facts.Hostname, m.wizard.hostname)
		return m, nil
	case "api-verify":
		if msg.err != nil {
			m.wizard.step = wizardStepError
			m.wizard.errorMessage = msg.err.Error()
			return m, nil
		}
		m.wizard.apiResult = &msg.apiResult
		if msg.apiResult.Refusal != "" {
			m.wizard.zeroSecrets()
			m.wizard.step = wizardStepError
			m.wizard.errorMessage = msg.apiResult.Refusal
			return m, nil
		}
		if msg.apiResult.Diagnosis != "" {
			m.wizard.step = wizardStepDiagnose
			return m, nil
		}
		m.wizard.step = wizardStepDetected
		m.wizard.name = fallbackName(defaultAPIWizardName(m.wizard.hostType, msg.apiResult.Endpoint), msg.apiResult.Endpoint)
		return m, nil
	case "api-check":
		if msg.err != nil {
			m.wizard.step = wizardStepError
			m.wizard.errorMessage = msg.err.Error()
			return m, nil
		}
		m.wizard.apiCheck = &msg.apiResult
		if msg.apiResult.Refusal != "" {
			m.wizard.zeroSecrets()
			m.wizard.step = wizardStepError
			m.wizard.errorMessage = msg.apiResult.Refusal
			return m, nil
		}
		if msg.apiResult.Diagnosis != "" {
			m.wizard.step = wizardStepDiagnose
			return m, nil
		}
		m.wizard.step = wizardStepReview
		m.wizard.focus = 0
		return m, nil
	case "root-assist":
		core.ZeroBytes(m.wizard.rootPassword)
		m.wizard.rootPassword = nil
		if msg.err != nil {
			m.wizard.step = wizardStepError
			m.wizard.zeroSecrets()
			m.wizard.errorMessage = msg.err.Error()
			return m, nil
		}
		m.wizard.step = wizardStepDetecting
		return m, detectStandardCmd(m.ctx, m.client, m.wizard)
	case "bootstrap-started":
		if msg.err != nil {
			m.wizard.zeroSecrets()
			m.wizard.step = wizardStepError
			m.wizard.errorMessage = msg.err.Error()
			return m, nil
		}
		m.wizard.sessionID = msg.session.ID
		m.wizard.streamEvents = msg.events
		m.wizard.streamErrors = msg.errors
		return m, nextBootstrapEventCmd(msg.events, msg.errors)
	case "commit":
		m.wizard.zeroSecrets()
		if msg.err != nil {
			m.wizard.step = wizardStepError
			m.wizard.errorMessage = msg.err.Error()
			return m, nil
		}
		m.hostScreen = hostScreenList
		m.status = "Host saved: " + msg.host.Name
		m.hostLoading = true
		return m, withClearScreen(loadHostsCmd(m.ctx, m.client))
	}
	return m, nil
}

func (m model) handleBootstrapEvent(msg bootstrapEventMsg) (model, tea.Cmd) {
	if msg.err != nil {
		m.wizard.zeroSecrets()
		m.wizard.step = wizardStepError
		m.wizard.errorMessage = msg.err.Error()
		return m, nil
	}
	if msg.done {
		return m, nil
	}
	switch msg.event.Type {
	case "bootstrap.step":
		var event core.BootstrapEvent
		if err := json.Unmarshal(msg.event.Data, &event); err == nil {
			m.wizard.events = append(m.wizard.events, event)
		}
	case "bootstrap.complete":
		var summary core.BootstrapSummary
		if err := json.Unmarshal(msg.event.Data, &summary); err == nil {
			m.wizard.summary = &summary
			m.wizard.step = wizardStepReview
			m.wizard.focus = 0
			m.wizard.name = fallbackName(summary.Facts.Hostname, m.wizard.hostname)
			m.wizard.zeroSecrets()
			return m, nil
		}
	case "bootstrap.error":
		var payload map[string]string
		_ = json.Unmarshal(msg.event.Data, &payload)
		m.wizard.zeroSecrets()
		m.wizard.step = wizardStepError
		m.wizard.errorMessage = payload["error"]
		return m, nil
	}
	return m, nextBootstrapEventCmd(m.wizard.streamEvents, m.wizard.streamErrors)
}

func (m model) renderWizard(bodyHeight int) string {
	var content string
	switch m.wizard.step {
	case wizardStepInput:
		content = m.renderWizardInput()
	case wizardStepDetecting:
		label := "Connecting and detecting"
		if m.wizard.isAPI() {
			label = "Verifying API"
		}
		content = m.styles.Card.Width(66).Render(m.spinner.View() + " " + label)
	case wizardStepDetected:
		content = m.renderWizardDetected()
	case wizardStepDiagnose:
		if m.wizard.isAPI() {
			content = m.renderAPIWizardDiagnose()
		} else {
			content = m.renderWizardDiagnose()
		}
	case wizardStepPreinstall:
		content = m.renderWizardPreinstall()
	case wizardStepBootstrapping:
		if m.wizard.isAPI() {
			content = m.styles.Card.Width(72).Render(m.spinner.View() + " Calling update-check API")
		} else {
			content = m.renderWizardBootstrap(bodyHeight)
		}
	case wizardStepReview:
		content = m.renderWizardReview()
	case wizardStepError:
		content = m.styles.Card.Width(72).Render(m.styles.ErrorText.Render("Wizard stopped") + "\n\n" + m.wizard.errorMessage + "\n\nPress r to start over or Esc to return to Hosts.")
	}
	return m.centerCard(bodyHeight, content)
}

func (m model) renderWizardInput() string {
	types := []string{"Standard", "TrueNAS", "Home Assistant OS"}
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Add new host"))
	builder.WriteString("\n\nHost type: ")
	for index, label := range types {
		marker := "( ) "
		if index == m.wizard.hostType {
			marker = "(*) "
		}
		text := marker + label
		if m.wizard.focus == 0 && index == m.wizard.hostType {
			builder.WriteString(m.styles.ActiveTab.Render(text))
		} else {
			builder.WriteString(text)
		}
		builder.WriteString("  ")
	}
	builder.WriteString("\n\n")
	if m.wizard.isAPI() {
		endpoint := m.wizard.hostname
		if endpoint == "" {
			endpoint = "https://"
		}
		builder.WriteString(m.inputLine("URL", endpoint, m.wizard.focus == 1, false))
		builder.WriteByte('\n')
		builder.WriteString(m.inputLine("API token", strings.Repeat("*", len(m.wizard.password)), m.wizard.focus == 2, true))
		builder.WriteByte('\n')
		builder.WriteString(m.styles.MutedText.Render(apiTokenHint(m.wizard.hostType)))
	} else {
		builder.WriteString(m.inputLine("Hostname or IP", m.wizard.hostname, m.wizard.focus == 1, false))
		builder.WriteByte('\n')
		builder.WriteString(m.inputLine("SSH user", m.wizard.sshUser, m.wizard.focus == 2, false))
		builder.WriteByte('\n')
		builder.WriteString(m.inputLine("Password", strings.Repeat("*", len(m.wizard.password)), m.wizard.focus == 3, true))
	}
	builder.WriteString("\n\nEnter Go   Esc Cancel")
	return m.styles.Card.Width(74).Render(builder.String())
}

func (m model) renderWizardDetected() string {
	if m.wizard.isAPI() {
		return m.renderAPIWizardDetected()
	}
	facts := m.wizard.facts
	lines := []string{
		theme.SymbolOK + " Connected to " + m.wizard.hostname + " as " + m.wizard.sshUser,
		theme.SymbolOK + " OS: " + facts.OSPretty,
		theme.SymbolOK + " Package: " + string(facts.PackageManager),
		theme.SymbolOK + " Arch: " + facts.Arch,
	}
	if facts.IsProxmox {
		lines = append(lines, theme.SymbolWarning+" Proxmox VE detected - using apt-get dist-upgrade")
	}
	if !facts.PatchpilotUserExists {
		lines = append(lines, theme.SymbolWarning+" patchpilot user: not present, will be created")
	}
	lines = append(lines, "", "Enter to bootstrap   Esc cancel")
	return m.styles.Card.Width(76).Render(m.styles.CardTitle.Render("Detection summary") + "\n\n" + strings.Join(lines, "\n"))
}

func (m model) renderAPIWizardDetected() string {
	result := m.wizard.apiResult
	if result == nil {
		return m.styles.Card.Render("Preparing API verification")
	}
	lines := []string{
		theme.SymbolOK + " Endpoint: " + result.Endpoint,
		theme.SymbolOK + " Version: " + result.Version,
	}
	if len(result.Components) > 0 {
		for _, component := range result.Components {
			lines = append(lines, theme.SymbolOK+" "+component.Name+": "+component.Version)
		}
	}
	lines = append(lines, "", "Enter to check updates   Esc cancel")
	return m.styles.Card.Width(78).Render(m.styles.CardTitle.Render("API verification") + "\n\n" + strings.Join(lines, "\n"))
}

func (m model) renderAPIWizardDiagnose() string {
	diagnosis := "API call failed"
	if m.wizard.apiResult != nil && m.wizard.apiResult.Diagnosis != "" {
		diagnosis = m.wizard.apiResult.Diagnosis
	}
	if m.wizard.apiCheck != nil && m.wizard.apiCheck.Diagnosis != "" {
		diagnosis = m.wizard.apiCheck.Diagnosis
	}
	return m.styles.Card.Width(72).Render(m.styles.ErrorText.Render(theme.SymbolError+" "+diagnosis) + "\n\nr retry   e edit   Esc cancel")
}

func (m model) renderWizardDiagnose() string {
	diagnosis := m.wizard.diagnosis
	var builder strings.Builder
	builder.WriteString(m.styles.ErrorText.Render(theme.SymbolError + " Permission diagnosis"))
	builder.WriteString("\n\n")
	builder.WriteString(diagnosis.Reason)
	builder.WriteString("\n\n")
	builder.WriteString(diagnosis.PathForward)
	builder.WriteString("\n\nRoot password: ")
	builder.WriteString(strings.Repeat("*", len(m.wizard.rootPassword)))
	builder.WriteString("\n\nManual fix:\n")
	for _, command := range diagnosis.ManualFix {
		builder.WriteString("  ")
		builder.WriteString(command)
		builder.WriteByte('\n')
	}
	builder.WriteString("\nf Fix automatically   p Pre-install mode   Esc cancel")
	return m.styles.Card.Width(76).Render(builder.String())
}

func (m model) renderWizardPreinstall() string {
	manager := m.wizard.facts.PackageManager
	commands := []string{
		"install -d -m 0700 -o patchpilot -g patchpilot /home/patchpilot/.ssh",
		"# paste the orchestrator public key into /home/patchpilot/.ssh/authorized_keys",
		"# resolve package-manager paths with command -v before writing sudoers",
		"visudo -cf /etc/sudoers.d/patchpilot",
	}
	return m.styles.Card.Width(76).Render(m.styles.CardTitle.Render("Pre-install mode") + "\n\nPackage manager: " + string(manager) + "\n\n" + strings.Join(commands, "\n") + "\n\nAfter installing the key and sudoers entry, press r to retry detection.")
}

func (m model) renderWizardBootstrap(bodyHeight int) string {
	lines := make([]string, 0, max(1, len(m.wizard.events)))
	if len(m.wizard.events) == 0 {
		lines = append(lines, m.spinner.View()+" Waiting for bootstrap stream")
	}
	for _, event := range m.wizard.events {
		symbol := theme.SymbolPending
		if event.Status == core.BootstrapStepSuccess {
			symbol = theme.SymbolOK
		}
		if event.Status == core.BootstrapStepFailed {
			symbol = theme.SymbolError
		}
		line := symbol + " " + event.Step
		if event.Message != "" {
			line += " - " + event.Message
		}
		lines = append(lines, line)
	}
	visible := RenderClampedLines(ViewportInput{Lines: lines, Width: 72, Height: max(1, bodyHeight-7)})
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Bootstrap progress"))
	builder.WriteString("\n\n")
	for _, line := range visible {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return m.styles.Card.Width(78).Render(builder.String())
}

func (m model) renderWizardReview() string {
	if m.wizard.isAPI() {
		return m.renderAPIWizardReview()
	}
	summary := m.wizard.summary
	if summary == nil {
		return m.styles.Card.Render("Preparing review")
	}
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Review and save"))
	builder.WriteString("\n\n")
	builder.WriteString(m.inputLine("Host", m.wizard.name, m.wizard.focus == 0, false))
	builder.WriteByte('\n')
	builder.WriteString("Type: Standard")
	if summary.Facts.IsProxmox {
		builder.WriteString(" / Proxmox VE")
	}
	builder.WriteString("\nPackage: ")
	builder.WriteString(string(summary.Facts.PackageManager))
	builder.WriteString("\nSSH user: patchpilot")
	builder.WriteString("\nSchedule: Manual only")
	builder.WriteString("\n")
	builder.WriteString(m.inputLine("Notes", m.wizard.notes, m.wizard.focus == 1, false))
	builder.WriteString("\nEnabled: ")
	check := "[ ]"
	if m.wizard.enabled {
		check = "[x]"
	}
	if m.wizard.focus == 2 {
		builder.WriteString(m.styles.ActiveTab.Render(check))
	} else {
		builder.WriteString(check)
	}
	builder.WriteString("\n\nReboot policy:\n")
	reportOnly := "( ) Report only"
	auto := "( ) Auto-reboot after successful update"
	if m.wizard.autoReboot {
		auto = "(*) Auto-reboot after successful update"
	} else {
		reportOnly = "(*) Report only"
	}
	policy := reportOnly + "  " + auto
	if m.wizard.focus == 3 {
		builder.WriteString(m.styles.ActiveTab.Render(policy))
	} else {
		builder.WriteString(policy)
	}
	builder.WriteByte('\n')
	countdown := fmt.Sprintf("Countdown: [ %d ] seconds", core.NormalizeRebootDelay(m.wizard.rebootDelay))
	if m.wizard.focus == 4 && m.wizard.autoReboot {
		builder.WriteString(m.styles.ActiveTab.Render(countdown))
	} else if m.wizard.autoReboot {
		builder.WriteString(countdown)
	} else {
		builder.WriteString(m.styles.MutedText.Render(countdown))
	}
	fmt.Fprintf(&builder, "\n\nDry run: %s\n", summary.DryRun.Summary)
	builder.WriteString("\nEnter Save   Space toggle   ←/→ countdown   Esc cancel")
	return m.styles.Card.Width(78).Render(builder.String())
}

func (m model) renderAPIWizardReview() string {
	result := m.wizard.apiCheck
	if result == nil {
		result = m.wizard.apiResult
	}
	if result == nil {
		return m.styles.Card.Render("Preparing review")
	}
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Review and save"))
	builder.WriteString("\n\n")
	builder.WriteString(m.inputLine("Host", m.wizard.name, m.wizard.focus == 0, false))
	builder.WriteByte('\n')
	builder.WriteString("Type: ")
	builder.WriteString(apiWizardLabel(m.wizard.hostType))
	builder.WriteString("\nEndpoint: ")
	builder.WriteString(result.Endpoint)
	builder.WriteString("\nVersion: ")
	builder.WriteString(result.Version)
	builder.WriteString("\nLast check: ")
	builder.WriteString(result.LastCheckStatus)
	for _, component := range result.Components {
		builder.WriteString("\n")
		builder.WriteString(component.Name)
		builder.WriteString(": ")
		builder.WriteString(component.Version)
		if component.LatestVersion != "" && component.LatestVersion != component.Version {
			builder.WriteString(" -> ")
			builder.WriteString(component.LatestVersion)
		}
	}
	builder.WriteByte('\n')
	builder.WriteString(m.inputLine("Notes", m.wizard.notes, m.wizard.focus == 1, false))
	builder.WriteString("\nEnabled: ")
	check := "[ ]"
	if m.wizard.enabled {
		check = "[x]"
	}
	if m.wizard.focus == 2 {
		builder.WriteString(m.styles.ActiveTab.Render(check))
	} else {
		builder.WriteString(check)
	}
	builder.WriteString("\nReboot policy: ")
	if m.wizard.autoReboot {
		fmt.Fprintf(&builder, "auto-reboot (%ds)", core.NormalizeRebootDelay(m.wizard.rebootDelay))
	} else {
		builder.WriteString("report only")
	}
	builder.WriteString("\n\nEnter Save   Space toggle enabled   Esc cancel")
	return m.styles.Card.Width(82).Render(builder.String())
}

func (m model) inputLine(label string, value string, focused bool, secret bool) string {
	field := fmt.Sprintf("%-15s [%s]", label+":", value)
	if secret && value == "" {
		field = fmt.Sprintf("%-15s [%s]", label+":", "")
	}
	if focused {
		return m.styles.ActiveTab.Render(field)
	}
	return field
}

func (w *standardWizard) appendFocused(value string) {
	switch w.focus {
	case 1:
		w.hostname += value
	case 2:
		if w.isAPI() {
			w.password = append(w.password, []byte(value)...)
			return
		}
		w.sshUser += value
	case 3:
		w.password = append(w.password, []byte(value)...)
	}
}

func (w *standardWizard) backspaceFocused() {
	switch w.focus {
	case 1:
		if len(w.hostname) > 0 {
			w.hostname = w.hostname[:len(w.hostname)-1]
		}
	case 2:
		if w.isAPI() {
			if len(w.password) > 0 {
				w.password[len(w.password)-1] = 0
				w.password = w.password[:len(w.password)-1]
			}
			return
		}
		if len(w.sshUser) > 0 {
			w.sshUser = w.sshUser[:len(w.sshUser)-1]
		}
	case 3:
		if len(w.password) > 0 {
			w.password[len(w.password)-1] = 0
			w.password = w.password[:len(w.password)-1]
		}
	}
}

func (w standardWizard) isAPI() bool {
	return w.hostType == 1 || w.hostType == 2
}

func (w standardWizard) inputFocusCount() int {
	if w.isAPI() {
		return 3
	}
	return 4
}

func (w standardWizard) selectedAPIHostType() core.HostType {
	if w.hostType == 1 {
		return core.HostTypeTrueNAS
	}
	if w.hostType == 2 {
		return core.HostTypeHAOS
	}
	return core.HostTypeStandard
}

func (w *standardWizard) zeroSecrets() {
	core.ZeroBytes(w.password)
	core.ZeroBytes(w.rootPassword)
	w.password = nil
	w.rootPassword = nil
}

func detectStandardCmd(ctx context.Context, client *ipc.Client, wizard standardWizard) tea.Cmd {
	input := core.StandardDetectInput{Hostname: strings.TrimSpace(wizard.hostname), SSHPort: 22, SSHUser: strings.TrimSpace(wizard.sshUser), Password: append([]byte{}, wizard.password...)}
	return func() tea.Msg {
		defer core.ZeroBytes(input.Password)
		requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		result, err := client.DetectStandard(requestCtx, input)
		return wizardMsg{kind: "detect", detectResult: result, err: err}
	}
}

func rootAssistCmd(ctx context.Context, client *ipc.Client, wizard standardWizard) tea.Cmd {
	input := core.RootAssistInput{Hostname: strings.TrimSpace(wizard.hostname), SSHPort: 22, SSHUser: strings.TrimSpace(wizard.sshUser), RootPassword: append([]byte{}, wizard.rootPassword...), Facts: wizard.facts, Diagnosis: wizard.diagnosis}
	return func() tea.Msg {
		defer core.ZeroBytes(input.RootPassword)
		requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		err := client.RootAssist(requestCtx, input)
		return wizardMsg{kind: "root-assist", err: err}
	}
}

func startBootstrapCmd(ctx context.Context, client *ipc.Client, wizard standardWizard) tea.Cmd {
	input := core.BootstrapStartInput{Hostname: strings.TrimSpace(wizard.hostname), SSHPort: 22, SSHUser: strings.TrimSpace(wizard.sshUser), Password: append([]byte{}, wizard.password...), Facts: wizard.facts}
	return func() tea.Msg {
		defer core.ZeroBytes(input.Password)
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		session, err := client.StartBootstrap(requestCtx, input)
		if err != nil {
			return wizardMsg{kind: "bootstrap-started", err: err}
		}
		events, errors := client.WizardEvents(ctx, session.ID)
		return wizardMsg{kind: "bootstrap-started", session: session, events: events, errors: errors}
	}
}

func verifyAPIHostCmd(ctx context.Context, client *ipc.Client, wizard standardWizard) tea.Cmd {
	input := core.APIHostVerifyInput{Type: wizard.selectedAPIHostType(), Endpoint: strings.TrimSpace(wizard.hostname), APIToken: append([]byte{}, wizard.password...)}
	return func() tea.Msg {
		defer core.ZeroBytes(input.APIToken)
		requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		result, err := client.VerifyAPIHost(requestCtx, input)
		return wizardMsg{kind: "api-verify", apiResult: result, err: err}
	}
}

func checkAPIHostCmd(ctx context.Context, client *ipc.Client, wizard standardWizard) tea.Cmd {
	input := core.APIHostVerifyInput{Type: wizard.selectedAPIHostType(), Endpoint: strings.TrimSpace(wizard.hostname), APIToken: append([]byte{}, wizard.password...)}
	return func() tea.Msg {
		defer core.ZeroBytes(input.APIToken)
		requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		result, err := client.CheckAPIHost(requestCtx, input)
		return wizardMsg{kind: "api-check", apiResult: result, err: err}
	}
}

func nextBootstrapEventCmd(events <-chan stream.Event, errors <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case err, ok := <-errors:
			if ok && err != nil {
				return bootstrapEventMsg{err: err}
			}
			return bootstrapEventMsg{done: true}
		case event, ok := <-events:
			if !ok {
				return bootstrapEventMsg{done: true}
			}
			return bootstrapEventMsg{event: event}
		}
	}
}

func commitBootstrapCmd(ctx context.Context, client *ipc.Client, wizard standardWizard) tea.Cmd {
	input := core.CommitHostInput{SessionID: wizard.sessionID, Name: strings.TrimSpace(wizard.name), Notes: wizard.notes, Enabled: wizard.enabled, AutoReboot: wizard.autoReboot, AutoRebootDelay: core.NormalizeRebootDelay(wizard.rebootDelay)}
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		host, err := client.CommitBootstrap(requestCtx, input)
		return wizardMsg{kind: "commit", host: host, err: err}
	}
}

func commitAPIHostCmd(ctx context.Context, client *ipc.Client, wizard standardWizard) tea.Cmd {
	input := core.APIHostCommitInput{Type: wizard.selectedAPIHostType(), Endpoint: strings.TrimSpace(wizard.hostname), APIToken: append([]byte{}, wizard.password...), Name: strings.TrimSpace(wizard.name), Notes: wizard.notes, Enabled: wizard.enabled, AutoReboot: wizard.autoReboot, AutoRebootDelay: core.NormalizeRebootDelay(wizard.rebootDelay)}
	return func() tea.Msg {
		defer core.ZeroBytes(input.APIToken)
		requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		host, err := client.CommitAPIHost(requestCtx, input)
		return wizardMsg{kind: "commit", host: host, err: err}
	}
}

func cancelBootstrapCmd(ctx context.Context, client *ipc.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = client.CancelBootstrap(requestCtx, sessionID)
		return nil
	}
}

func fallbackName(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func apiWizardLabel(hostType int) string {
	if hostType == 1 {
		return "TrueNAS SCALE"
	}
	if hostType == 2 {
		return "Home Assistant OS"
	}
	return "Standard"
}

func apiTokenHint(hostType int) string {
	if hostType == 1 {
		return "TrueNAS SCALE API key"
	}
	if hostType == 2 {
		return "Home Assistant Long-Lived Access Token for /api/hassio"
	}
	return ""
}

func defaultAPIWizardName(hostType int, endpoint string) string {
	trimmed := strings.Trim(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://"), "/")
	if hostType == 1 {
		return "truenas-" + trimmed
	}
	if hostType == 2 {
		return "haos-" + trimmed
	}
	return trimmed
}
