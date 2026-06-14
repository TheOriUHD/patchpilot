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
)

type settingsLoadedMsg struct {
	snapshot core.SettingsSnapshot
	err      error
}

type settingsMutationMsg struct {
	message   string
	snapshot  *core.SettingsSnapshot
	conflicts []string
	err       error
}

func (m model) handleSettingsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settingsConfirm != "" {
		switch key.String() {
		case "y", "enter":
			confirm := m.settingsConfirm
			m.settingsConfirm = ""
			if confirm == "enable-concurrency" {
				return m, setConcurrencyCmd(m.ctx, m.client, true)
			}
			if confirm == "rotate-key" {
				return m, rotateKeypairCmd(m.ctx, m.client)
			}
		case "n", "esc":
			m.settingsConfirm = ""
			m.status = "Cancelled"
		}
		return m, nil
	}
	if len(m.settingsImportConflicts) > 0 {
		switch key.String() {
		case "s":
			return m, importInventoryCmd(m.ctx, m.client, m.settingsImportPath, m.resolveImportConflicts(core.InventoryConflictSkip))
		case "o":
			return m, importInventoryCmd(m.ctx, m.client, m.settingsImportPath, m.resolveImportConflicts(core.InventoryConflictOverwrite))
		case "r":
			return m, importInventoryCmd(m.ctx, m.client, m.settingsImportPath, m.renameImportConflicts())
		case "esc":
			m.settingsImportConflicts = nil
			m.status = "Import cancelled"
		}
		return m, nil
	}
	if m.settingsPathFocus != "" {
		switch key.String() {
		case "esc":
			m.settingsPathFocus = ""
			return m, nil
		case "backspace":
			if m.settingsPathFocus == "export" && len(m.settingsExportPath) > 0 {
				m.settingsExportPath = m.settingsExportPath[:len(m.settingsExportPath)-1]
			}
			if m.settingsPathFocus == "import" && len(m.settingsImportPath) > 0 {
				m.settingsImportPath = m.settingsImportPath[:len(m.settingsImportPath)-1]
			}
			return m, nil
		case "enter":
			focus := m.settingsPathFocus
			m.settingsPathFocus = ""
			if focus == "export" {
				return m, exportInventoryCmd(m.ctx, m.client, m.settingsExportPath)
			}
			return m, importInventoryCmd(m.ctx, m.client, m.settingsImportPath, nil)
		}
		if len(key.Runes) > 0 {
			if m.settingsPathFocus == "export" {
				m.settingsExportPath += string(key.Runes)
			} else {
				m.settingsImportPath += string(key.Runes)
			}
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
		m.settingsLoading = true
		return m, loadSettingsCmd(m.ctx, m.client)
	case "t":
		if m.settingsSnapshot == nil {
			return m, nil
		}
		if !m.settingsSnapshot.ConcurrencyEnabled {
			m.settingsConfirm = "enable-concurrency"
			return m, nil
		}
		return m, setConcurrencyCmd(m.ctx, m.client, false)
	case "k":
		m.settingsConfirm = "rotate-key"
		return m, nil
	case "e":
		m.settingsPathFocus = "export"
		if m.settingsExportPath == "" {
			m.settingsExportPath = "./patchpilot-inventory.yaml"
		}
		return m, nil
	case "i":
		m.settingsPathFocus = "import"
		if m.settingsImportPath == "" {
			m.settingsImportPath = "./patchpilot-inventory.yaml"
		}
		return m, nil
	}
	return m, nil
}

func (m model) renderSettingsView(bodyHeight int) string {
	if m.settingsLoading && m.settingsSnapshot == nil {
		return m.centerCard(bodyHeight, m.styles.Card.Render(m.spinner.View()+" Loading settings"))
	}
	if m.settingsError != "" && m.settingsSnapshot == nil {
		return m.centerCard(bodyHeight, m.styles.Card.Width(64).Render(m.styles.ErrorText.Render("Could not load settings")+"\n\n"+m.settingsError))
	}
	if m.settingsSnapshot == nil {
		return m.centerCard(bodyHeight, m.styles.Card.Render("Settings unavailable"))
	}
	snapshot := *m.settingsSnapshot
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Settings"))
	builder.WriteString("\n\n")
	builder.WriteString(m.styles.CardTitle.Render("Daemon"))
	fmt.Fprintf(&builder, "\nData dir: %s\nSocket: %s\nLog: %s\n", snapshot.Paths.DataDir, snapshot.Paths.SocketPath, snapshot.Paths.LogPath)
	builder.WriteString(m.styles.MutedText.Render("Configured through environment variables or config.yaml."))
	builder.WriteString("\n\n")
	builder.WriteString(m.styles.CardTitle.Render("Concurrency"))
	fmt.Fprintf(&builder, "\nExperimental parallel Group A updates: %v", snapshot.ConcurrencyEnabled)
	builder.WriteString("\n")
	builder.WriteString(m.styles.MutedText.Render("Only standard non-Proxmox Group A hosts may run in parallel. TrueNAS, HAOS, Proxmox, and self always stay sequential."))
	builder.WriteString("\n\n")
	builder.WriteString(m.styles.CardTitle.Render("SSH"))
	builder.WriteString("\nPublic key:\n")
	builder.WriteString(strings.TrimSpace(snapshot.PublicKey))
	builder.WriteString("\n\n")
	builder.WriteString(m.styles.CardTitle.Render("Notifications"))
	builder.WriteString("\n")
	builder.WriteString(m.styles.MutedText.Render("coming soon"))
	builder.WriteString("\n\n")
	builder.WriteString(m.styles.CardTitle.Render("Backup & Restore"))
	builder.WriteString("\nExport path: ")
	builder.WriteString(focusedText(m, "export", m.settingsExportPath))
	builder.WriteString("\nImport path: ")
	builder.WriteString(focusedText(m, "import", m.settingsImportPath))
	if len(m.settingsImportConflicts) > 0 {
		builder.WriteString("\n")
		builder.WriteString(m.styles.ErrorText.Render("Conflicts: " + strings.Join(m.settingsImportConflicts, ", ")))
		builder.WriteString("\ns skip  o overwrite  r rename  Esc cancel")
	}
	builder.WriteString("\n\n")
	builder.WriteString(m.styles.CardTitle.Render("Self-Update Logs"))
	if len(snapshot.SelfUpdateLogs) == 0 {
		builder.WriteString("\n-")
	} else {
		for _, log := range snapshot.SelfUpdateLogs {
			fmt.Fprintf(&builder, "\nrun %d  %s  %d bytes", log.RunID, log.ModifiedAt.Format("Jan 02 15:04"), log.Size)
		}
	}
	builder.WriteString("\n\n")
	builder.WriteString(m.styles.CardTitle.Render("Daemon Log"))
	logStart := max(0, len(snapshot.DaemonLogTail)-5)
	for _, line := range snapshot.DaemonLogTail[logStart:] {
		builder.WriteString("\n")
		builder.WriteString(truncate(line, 110))
	}
	builder.WriteString("\n\n")
	builder.WriteString(m.styles.CardTitle.Render("About"))
	fmt.Fprintf(&builder, "\nVersion: %s\nCommit: %s\nBuild date: %s\nGo: %s\nPlatform: %s", snapshot.About.Version, snapshot.About.Commit, snapshot.About.BuildDate, snapshot.About.GoVersion, snapshot.About.OSArch)
	if m.settingsConfirm != "" {
		builder.WriteString("\n\n")
		builder.WriteString(m.styles.ErrorText.Render(m.settingsConfirmText()))
	}
	return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Padding(1, 2).Render(builder.String())
}

func (m model) settingsConfirmText() string {
	if m.settingsConfirm == "enable-concurrency" {
		return "Enable experimental concurrency? Only Group A standard hosts may run in parallel; TrueNAS, HAOS, Proxmox, and self always stay sequential. y/n"
	}
	if m.settingsConfirm == "rotate-key" {
		names := []string{}
		for _, host := range m.hosts {
			if host.Enabled && host.Type == core.HostTypeStandard && !host.IsSelf {
				names = append(names, host.Name)
			}
		}
		return "Rotate SSH keypair for: " + strings.Join(names, ", ") + "? y/n"
	}
	return "Confirm? y/n"
}

func focusedText(m model, focus string, value string) string {
	if value == "" {
		value = "-"
	}
	if m.settingsPathFocus == focus {
		return m.styles.ActiveTab.Render(value)
	}
	return value
}

func (m model) resolveImportConflicts(action core.InventoryConflictAction) map[string]core.InventoryConflictResolution {
	resolutions := map[string]core.InventoryConflictResolution{}
	for _, name := range m.settingsImportConflicts {
		resolutions[name] = core.InventoryConflictResolution{Action: action}
	}
	return resolutions
}

func (m model) renameImportConflicts() map[string]core.InventoryConflictResolution {
	resolutions := map[string]core.InventoryConflictResolution{}
	for _, name := range m.settingsImportConflicts {
		resolutions[name] = core.InventoryConflictResolution{Action: core.InventoryConflictRename, NewName: name + " imported"}
	}
	return resolutions
}

func (m model) settingsKeybinds() []string {
	if m.settingsConfirm != "" {
		return []string{"y confirm", "n cancel"}
	}
	if m.settingsPathFocus != "" {
		return []string{"Enter apply", "Esc cancel"}
	}
	if len(m.settingsImportConflicts) > 0 {
		return []string{"s skip", "o overwrite", "r rename", "Esc cancel"}
	}
	return []string{"t concurrency", "k rotate key", "e export", "i import", "r refresh"}
}

func loadSettingsCmd(ctx context.Context, client *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		snapshot, err := client.Settings(requestCtx)
		return settingsLoadedMsg{snapshot: snapshot, err: err}
	}
}

func setConcurrencyCmd(ctx context.Context, client *ipc.Client, enabled bool) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		snapshot, err := client.SetConcurrency(requestCtx, enabled)
		return settingsMutationMsg{message: "Concurrency setting updated", snapshot: &snapshot, err: err}
	}
}

func rotateKeypairCmd(ctx context.Context, client *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		err := client.RotateKeypair(requestCtx)
		return settingsMutationMsg{message: "SSH key rotation complete", err: err}
	}
}

func exportInventoryCmd(ctx context.Context, client *ipc.Client, path string) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		err := client.ExportInventory(requestCtx, path)
		return settingsMutationMsg{message: "Inventory exported", err: err}
	}
}

func importInventoryCmd(ctx context.Context, client *ipc.Client, path string, conflicts map[string]core.InventoryConflictResolution) tea.Cmd {
	return func() tea.Msg {
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		result, err := client.ImportInventory(requestCtx, core.InventoryPathRequest{Path: path, Conflicts: conflicts})
		message := fmt.Sprintf("Inventory imported: %d hosts, %d schedules", result.ImportedHosts, result.ImportedSchedules)
		return settingsMutationMsg{message: message, conflicts: result.Conflicts, err: err}
	}
}
