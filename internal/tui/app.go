package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/config"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/ipc"
	"github.com/TheOriUHD/patchpilot/internal/stream"
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

const (
	minWidth  = 80
	minHeight = 24
)

// Run starts the fullscreen patchpilot TUI.
func Run(ctx context.Context) (err error) {
	socketPath, err := config.ClientSocketPath()
	if err != nil {
		return fmt.Errorf("resolve daemon socket: %w", err)
	}

	var program *tea.Program
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if program != nil {
			program.Kill()
		}
		err = fmt.Errorf("tui recovered from panic: %v", recovered)
	}()

	model := newModel(ctx, socketPath)
	program = tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err = program.Run(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}

type model struct {
	ctx          context.Context
	client       *ipc.Client
	styles       theme.Styles
	spinner      spinner.Model
	tabs         []string
	activeTab    int
	width        int
	height       int
	daemonState  components.DaemonState
	status       string
	daemonError  string
	starting     bool
	healthLoaded bool

	hosts        []core.Host
	hostLoading  bool
	hostError    string
	hostSelected int
	hostFilter   string
	filtering    bool
	hostScreen   hostScreen
	detailTab    int
	renameMode   bool
	renameBuffer string
	pending      pendingAction
	wizard       standardWizard

	schedules        []core.Schedule
	scheduleLoading  bool
	scheduleError    string
	scheduleSelected int
	scheduleScreen   scheduleScreen
	scheduleEditor   scheduleEditor
	schedulePending  schedulePendingAction

	runs               []core.Run
	runLoading         bool
	runError           string
	runSelected        int
	liveRun            *core.RunDetail
	liveLogLines       []liveLogEntry
	liveHostIndex      int
	liveModalVisible   bool
	liveMinimized      bool
	liveCompleted      bool
	liveScrollOffset   int
	liveStartedAt      time.Time
	liveCancel         bool
	runEvents          <-chan stream.Event
	runEventErrors     <-chan error
	runStreamStop      context.CancelFunc
	runBanner          string
	liveAvailableRunID int64
	runTreeDetail      *core.RunDetail
	runTreeLogs        []core.RunLogEntry
	runTreeHostIndex   int
	logViewer          *historyLogViewer
	autoReboot         autoRebootState
	runOffset          int
	runDateRange       string
	runStatusFilters   map[core.RunStatus]bool
	runTriggerFilters  map[core.RunTrigger]bool

	settingsLoading         bool
	settingsError           string
	settingsSnapshot        *core.SettingsSnapshot
	settingsConfirm         string
	settingsExportPath      string
	settingsImportPath      string
	settingsPathFocus       string
	settingsImportConflicts []string
}

type healthLoadedMsg struct {
	health ipc.HealthResponse
	err    error
}

type daemonStartedMsg struct {
	err error
}

type runPollTickMsg struct{}

type autoRebootTickMsg struct{}

type hostRebootMsg struct {
	hostID int64
	err    error
}

func newModel(ctx context.Context, socketPath string) model {
	styles := theme.NewStyles()
	return model{
		ctx:         ctx,
		client:      ipc.NewClient(socketPath),
		styles:      styles,
		spinner:     components.NewSpinner(styles),
		tabs:        []string{"Hosts", "Schedules", "Logs", "Settings"},
		daemonState: components.DaemonChecking,
		status:      "",
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, checkHealthCmd(m.ctx, m.client))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(typed)
	case spinner.TickMsg:
		if m.daemonState == components.DaemonChecking || m.starting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(typed)
			return m, cmd
		}
		return m, nil
	case healthLoadedMsg:
		m.healthLoaded = true
		m.starting = false
		if typed.err != nil {
			m.daemonState = components.DaemonDown
			m.daemonError = typed.err.Error()
			m.status = "Daemon is not reachable"
			return m, nil
		}
		m.daemonState = components.DaemonUp
		m.daemonError = ""
		m.status = "Connected to daemon " + typed.health.Version
		m.hostLoading = true
		m.scheduleLoading = true
		m.runLoading = true
		m.settingsLoading = true
		return m, tea.Batch(loadHostsCmd(m.ctx, m.client), loadSchedulesCmd(m.ctx, m.client), loadRunsCmd(m.ctx, m.client), loadSettingsCmd(m.ctx, m.client), runPollCmd())
	case daemonStartedMsg:
		m.starting = false
		if typed.err != nil {
			m.daemonState = components.DaemonDown
			m.daemonError = typed.err.Error()
			m.status = "Could not start daemon"
			return m, nil
		}
		m.daemonState = components.DaemonChecking
		m.status = "Daemon start requested"
		return m, tea.Batch(m.spinner.Tick, checkHealthCmd(m.ctx, m.client))
	case hostsLoadedMsg:
		m.hostLoading = false
		if typed.err != nil {
			m.hostError = typed.err.Error()
			m.status = "Could not load hosts"
			return m, nil
		}
		m.hostError = ""
		m.hosts = typed.hosts
		if m.hostSelected >= len(m.filteredHosts()) {
			m.hostSelected = max(0, len(m.filteredHosts())-1)
		}
		return m, nil
	case hostMutationMsg:
		m.pending = pendingAction{}
		if typed.err != nil {
			m.status = typed.err.Error()
			return m, nil
		}
		m.status = typed.message
		m.hostLoading = true
		return m, loadHostsCmd(m.ctx, m.client)
	case schedulesLoadedMsg:
		m.scheduleLoading = false
		if typed.err != nil {
			m.scheduleError = typed.err.Error()
			m.status = "Could not load schedules"
			return m, nil
		}
		m.scheduleError = ""
		m.schedules = typed.schedules
		if m.scheduleSelected >= len(m.schedules) {
			m.scheduleSelected = max(0, len(m.schedules)-1)
		}
		return m, nil
	case scheduleMutationMsg:
		if typed.err != nil {
			m.status = typed.err.Error()
			return m, nil
		}
		m.status = typed.message
		m.scheduleScreen = scheduleScreenList
		m.scheduleLoading = true
		return m, loadSchedulesCmd(m.ctx, m.client)
	case runsLoadedMsg:
		m.runLoading = false
		if typed.err != nil {
			m.runError = typed.err.Error()
			m.status = "Could not load runs"
			return m, nil
		}
		m.runError = ""
		m.runs = typed.runs
		m.observeActiveRun()
		if m.runSelected >= len(m.runs) {
			m.runSelected = max(0, len(m.runs)-1)
		}
		return m, nil
	case runStartedMsg:
		if typed.err != nil {
			m.status = typed.err.Error()
			return m, nil
		}
		m.openLiveRun(typed.detail)
		m.status = fmt.Sprintf("Run %d %s", typed.detail.Run.ID, typed.detail.Run.Status)
		return m, tea.Batch(m.startRunStream(typed.detail.Run.ID), loadRunsCmd(m.ctx, m.client))
	case runDetailLoadedMsg:
		if typed.err != nil {
			m.status = typed.err.Error()
			return m, nil
		}
		if typed.detail.Run.Status == core.RunStatusRunning || typed.detail.Run.Status == core.RunStatusQueued {
			m.openLiveRun(typed.detail)
			return m, m.startRunStream(typed.detail.Run.ID)
		}
		m.runTreeDetail = &typed.detail
		m.runTreeLogs = typed.logs
		m.runTreeHostIndex = 0
		return m, nil
	case runStreamMsg:
		return m, m.handleRunStreamEvent(typed.event)
	case runStreamErrorMsg:
		if typed.err != nil && m.liveRun != nil {
			m.status = typed.err.Error()
		}
		return m, nil
	case runCancelledMsg:
		m.liveCancel = false
		if typed.err != nil {
			m.status = typed.err.Error()
			return m, nil
		}
		m.status = "Run cancelled"
		return m, loadRunsCmd(m.ctx, m.client)
	case runPollTickMsg:
		if m.daemonState != components.DaemonUp {
			return m, runPollCmd()
		}
		return m, tea.Batch(loadRunsCmd(m.ctx, m.client), runPollCmd())
	case autoRebootTickMsg:
		return m.handleAutoRebootTick()
	case hostRebootMsg:
		return m.handleHostRebootMsg(typed)
	case dryRunLoadedMsg:
		if typed.err != nil {
			m.status = typed.err.Error()
			return m, nil
		}
		m.status = typed.result.Summary
		return m, nil
	case settingsLoadedMsg:
		m.settingsLoading = false
		if typed.err != nil {
			m.settingsError = typed.err.Error()
			m.status = "Could not load settings"
			return m, nil
		}
		m.settingsError = ""
		m.settingsSnapshot = &typed.snapshot
		return m, nil
	case settingsMutationMsg:
		m.settingsConfirm = ""
		m.settingsLoading = false
		if typed.err != nil {
			m.status = typed.err.Error()
			return m, nil
		}
		if typed.snapshot != nil {
			m.settingsSnapshot = typed.snapshot
		}
		if len(typed.conflicts) > 0 {
			m.settingsImportConflicts = typed.conflicts
			m.status = "Import conflicts need a choice"
			return m, nil
		}
		m.settingsImportConflicts = nil
		m.status = typed.message
		return m, loadSettingsCmd(m.ctx, m.client)
	case wizardMsg:
		var cmd tea.Cmd
		m, cmd = m.handleWizardMsg(typed)
		return m, cmd
	case bootstrapEventMsg:
		var cmd tea.Cmd
		m, cmd = m.handleBootstrapEvent(typed)
		return m, cmd
	default:
		return m, nil
	}
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		message := fmt.Sprintf("Terminal too small (need %dx%d, current %dx%d)", minWidth, minHeight, m.width, m.height)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.styles.ErrorText.Render(message))
	}

	tabbar := strings.TrimSuffix(components.RenderTabBar(m.styles, components.TabBarState{Labels: m.tabs, Active: m.activeTab}), "\n")
	status := components.RenderStatusBar(m.styles, components.StatusBarState{
		Keybinds: m.keybinds(),
		Daemon:   m.daemonState,
		Message:  m.status,
		Width:    m.width,
	})

	bodyHeight := m.height - lipgloss.Height(tabbar) - lipgloss.Height(status)
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	bodyContent := m.renderActiveTab(bodyHeight)
	if m.runBanner != "" && !m.liveModalVisible {
		bodyContent = m.renderRunBanner() + "\n" + bodyContent
	}
	if m.logViewer != nil {
		bodyContent = m.renderHistoryLogViewer(bodyHeight)
	} else if m.liveRun != nil && m.liveModalVisible {
		bodyContent = m.renderLiveRunOverlay(bodyHeight)
	}
	body := lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Render(bodyContent)
	return lipgloss.JoinVertical(lipgloss.Left, tabbar, body, status)
}

func (m model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.logViewer != nil {
		return m.handleHistoryLogViewerKey(key)
	}
	if m.liveRun != nil && m.liveModalVisible {
		return m.handleLiveRunKey(key)
	}
	if key.String() == "l" || key.String() == "L" {
		if m.liveRun != nil {
			m.liveModalVisible = true
			m.liveMinimized = false
			m.runBanner = ""
			return m, nil
		}
		if m.liveAvailableRunID != 0 {
			return m, loadRunDetailCmd(m.ctx, m.client, m.liveAvailableRunID)
		}
	}
	if m.daemonState == components.DaemonUp && m.activeTab == 0 {
		return m.handleHostsKey(key)
	}
	if m.daemonState == components.DaemonUp && m.activeTab == 1 {
		return m.handleSchedulesKey(key)
	}
	if m.daemonState == components.DaemonUp && m.activeTab == 2 {
		return m.handleRunsKey(key)
	}
	if m.daemonState == components.DaemonUp && m.activeTab == 3 {
		return m.handleSettingsKey(key)
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
	case "s":
		if m.daemonState != components.DaemonDown || m.starting {
			return m, nil
		}
		m.starting = true
		m.daemonState = components.DaemonChecking
		m.status = "Starting daemon"
		return m, tea.Batch(m.spinner.Tick, startDaemonCmd(m.ctx))
	default:
		return m, nil
	}
}

func (m model) renderActiveTab(bodyHeight int) string {
	if m.daemonState == components.DaemonChecking && !m.healthLoaded {
		return m.centerCard(bodyHeight, m.styles.Card.Render(m.spinner.View()+" Checking daemon"))
	}
	if m.daemonState == components.DaemonDown {
		return m.renderDaemonDown(bodyHeight)
	}

	switch m.activeTab {
	case 0:
		return m.renderHostsView(bodyHeight)
	case 1:
		return m.renderSchedulesView(bodyHeight)
	case 2:
		return m.renderRunsView(bodyHeight)
	case 3:
		return m.renderSettingsView(bodyHeight)
	default:
		return ""
	}
}

func (m model) renderDaemonDown(bodyHeight int) string {
	var builder strings.Builder
	builder.WriteString(m.styles.CardTitle.Render("Daemon not running"))
	builder.WriteString("\n\n")
	builder.WriteString("patchpilot could not reach the local daemon at:\n")
	builder.WriteString(m.styles.MutedText.Render(m.client.SocketPath()))
	builder.WriteString("\n\n")
	builder.WriteString("Press ")
	builder.WriteString(m.styles.CardTitle.Render("s"))
	builder.WriteString(" to try: ")
	builder.WriteString(daemonStartAction())
	builder.WriteString("\n")
	builder.WriteString("Manual start: ")
	builder.WriteString(m.styles.MutedText.Render(daemonManualCommand()))
	if m.daemonError != "" {
		builder.WriteString("\n\n")
		builder.WriteString(m.styles.ErrorText.Render("Reason: "))
		builder.WriteString(m.daemonError)
	}
	if m.starting {
		builder.WriteString("\n\n")
		builder.WriteString(m.spinner.View())
		builder.WriteString(" Starting daemon")
	}
	return m.centerCard(bodyHeight, m.styles.Card.Width(66).Render(builder.String()))
}

func (m model) centerCard(bodyHeight int, content string) string {
	return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, content)
}

func (m model) keybinds() []string {
	if m.daemonState == components.DaemonUp && m.activeTab == 0 {
		return m.hostKeybinds()
	}
	if m.daemonState == components.DaemonUp && m.activeTab == 1 {
		return m.scheduleKeybinds()
	}
	if m.daemonState == components.DaemonUp && m.activeTab == 2 {
		return m.runKeybinds()
	}
	if m.daemonState == components.DaemonUp && m.activeTab == 3 {
		return m.settingsKeybinds()
	}
	keys := []string{"Tab switch", "1-4 tabs", "q quit"}
	if m.daemonState == components.DaemonDown {
		keys = append([]string{"s start daemon"}, keys...)
	}
	return keys
}

func (m *model) clearConfirmations() {
	m.pending = pendingAction{}
	m.schedulePending = schedulePendingAction{}
	m.settingsConfirm = ""
	m.liveCancel = false
}

func loadHostsCmd(ctx context.Context, client *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		hosts, err := client.ListHosts(loadCtx)
		return hostsLoadedMsg{hosts: hosts, err: err}
	}
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func checkHealthCmd(ctx context.Context, client *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		healthCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		defer cancel()

		health, err := client.Health(healthCtx)
		return healthLoadedMsg{health: health, err: err}
	}
}

func startDaemonCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		startCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()

		return daemonStartedMsg{err: startDaemon(startCtx)}
	}
}
