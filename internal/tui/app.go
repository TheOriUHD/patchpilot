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
	"github.com/TheOriUHD/patchpilot/internal/ipc"
	"github.com/TheOriUHD/patchpilot/internal/tui/components"
	"github.com/TheOriUHD/patchpilot/internal/tui/theme"
)

const (
	minWidth  = 80
	minHeight = 24
)

// Run starts the fullscreen patchpilot TUI.
func Run(ctx context.Context) (err error) {
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
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

	model := newModel(ctx, cfg)
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
	cfg          config.Config
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
}

type healthLoadedMsg struct {
	health ipc.HealthResponse
	err    error
}

type daemonStartedMsg struct {
	err error
}

func newModel(ctx context.Context, cfg config.Config) model {
	styles := theme.NewStyles()
	return model{
		ctx:         ctx,
		cfg:         cfg,
		client:      ipc.NewClient(cfg.Paths.SocketPath),
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
		return m, nil
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

	tabbar := components.RenderTabBar(m.styles, components.TabBarState{Labels: m.tabs, Active: m.activeTab})
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
	body := lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Render(m.renderActiveTab(bodyHeight))
	return lipgloss.JoinVertical(lipgloss.Left, tabbar, body, status)
}

func (m model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m.renderHosts(bodyHeight)
	case 1:
		return m.centerCard(bodyHeight, m.styles.Card.Render(m.styles.CardTitle.Render("Schedules")+"\n\nNo schedules yet."))
	case 2:
		return m.centerCard(bodyHeight, m.styles.Card.Render(m.styles.CardTitle.Render("Logs")+"\n\nNo runs have been recorded yet."))
	case 3:
		return m.centerCard(bodyHeight, m.styles.Card.Render(m.styles.CardTitle.Render("Settings")+"\n\nDaemon socket: "+m.cfg.Paths.SocketPath))
	default:
		return ""
	}
}

func (m model) renderHosts(bodyHeight int) string {
	card := m.styles.Card.Width(52).Render(
		m.styles.CardTitle.Render("No hosts yet") + "\n\n" +
			"Daemon connection is healthy. Host inventory is empty.",
	)
	return m.centerCard(bodyHeight, card)
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
	keys := []string{"Tab switch", "1-4 tabs", "q quit"}
	if m.daemonState == components.DaemonDown {
		keys = append([]string{"s start daemon"}, keys...)
	}
	return keys
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
