package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/TheOriUHD/patchpilot/internal/pkgmgr"
	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
	"github.com/TheOriUHD/patchpilot/internal/stream"
)

// HostRepository is the storage interface required by host services.
type HostRepository interface {
	List(ctx context.Context) ([]Host, error)
	Get(ctx context.Context, id int64) (Host, error)
	Create(ctx context.Context, input HostCreate) (Host, error)
	Update(ctx context.Context, id int64, input HostUpdate) (Host, error)
	SetEnabled(ctx context.Context, id int64, enabled bool) (Host, error)
	Delete(ctx context.Context, id int64) error
}

// AuditRepository is the storage interface required for audit logging.
type AuditRepository interface {
	Create(ctx context.Context, event AuditEvent) error
}

// AuditEvent is a persistent operator/system audit event.
type AuditEvent struct {
	Actor      string
	Action     string
	TargetType string
	TargetID   *int64
	Details    string
	SourceIP   *string
}

// StandardDetectInput contains connection data for Standard host detection.
type StandardDetectInput struct {
	Hostname string `json:"hostname"`
	SSHPort  int    `json:"ssh_port"`
	SSHUser  string `json:"ssh_user"`
	Password []byte `json:"-"`
}

// PermissionDiagnosis explains why bootstrap cannot proceed yet.
type PermissionDiagnosis struct {
	Kind         string   `json:"kind"`
	Reason       string   `json:"reason"`
	PathForward  string   `json:"path_forward"`
	ManualFix    []string `json:"manual_fix"`
	CanAutoFix   bool     `json:"can_auto_fix"`
	PreinstallOK bool     `json:"preinstall_ok"`
}

// StandardDetectResult contains detection facts and any blocking diagnosis.
type StandardDetectResult struct {
	Facts     HostFacts            `json:"facts"`
	Refusal   string               `json:"refusal"`
	Diagnosis *PermissionDiagnosis `json:"diagnosis,omitempty"`
	DryRun    *pkgmgr.DryRunResult `json:"dry_run,omitempty"`
}

// RootAssistInput contains credentials for a root-assisted sudo fix.
type RootAssistInput struct {
	Hostname     string               `json:"hostname"`
	SSHPort      int                  `json:"ssh_port"`
	SSHUser      string               `json:"ssh_user"`
	RootPassword []byte               `json:"-"`
	Facts        HostFacts            `json:"facts"`
	Diagnosis    *PermissionDiagnosis `json:"diagnosis,omitempty"`
}

// BootstrapStartInput starts a Standard bootstrap stream.
type BootstrapStartInput struct {
	Hostname string    `json:"hostname"`
	SSHPort  int       `json:"ssh_port"`
	SSHUser  string    `json:"ssh_user"`
	Password []byte    `json:"-"`
	Facts    HostFacts `json:"facts"`
}

// BootstrapSession contains a created stream id.
type BootstrapSession struct {
	ID string `json:"id"`
}

// BootstrapSummary is emitted when a bootstrap stream completes successfully.
type BootstrapSummary struct {
	Facts         HostFacts           `json:"facts"`
	DryRun        pkgmgr.DryRunResult `json:"dry_run"`
	ResolvedPaths map[string]string   `json:"resolved_paths"`
	SudoersEntry  string              `json:"sudoers_entry"`
}

// CommitHostInput saves a completed wizard session to inventory.
type CommitHostInput struct {
	SessionID       string `json:"session_id"`
	Name            string `json:"name"`
	Notes           string `json:"notes"`
	Enabled         bool   `json:"enabled"`
	AutoReboot      bool   `json:"auto_reboot"`
	AutoRebootDelay int    `json:"auto_reboot_countdown_seconds"`
}

// HostService coordinates host inventory, detection, bootstrap, dry-runs, and manual updates.
type HostService struct {
	Hosts   HostRepository
	Audit   AuditRepository
	Keys    *remotessh.KeyStore
	Secrets SecretCipher
	Streams *stream.Hub
	Logger  *slog.Logger

	mu       sync.Mutex
	sessions map[string]bootstrapSessionState
}

// NewHostService creates the host workflow service.
func NewHostService(hosts HostRepository, audit AuditRepository, keys *remotessh.KeyStore, streams *stream.Hub, logger *slog.Logger, secrets ...SecretCipher) *HostService {
	var cipher SecretCipher
	if len(secrets) > 0 {
		cipher = secrets[0]
	}
	return &HostService{Hosts: hosts, Audit: audit, Keys: keys, Secrets: cipher, Streams: streams, Logger: logger, sessions: make(map[string]bootstrapSessionState)}
}

// DetectStandard probes a Standard host over SSH and diagnoses bootstrap readiness.
func (s *HostService) DetectStandard(ctx context.Context, input StandardDetectInput) (StandardDetectResult, error) {
	port := normalizePort(input.SSHPort)
	runner, err := remotessh.ConnectPassword(ctx, input.Hostname, port, input.SSHUser, input.Password)
	if err != nil {
		return StandardDetectResult{}, fmt.Errorf("connect to %s as %s: %w", input.Hostname, input.SSHUser, err)
	}
	defer func() { _ = runner.Close() }()

	result, err := runner.Run(ctx, StandardDetectionProbe, nil)
	if err != nil {
		return StandardDetectResult{}, fmt.Errorf("run detection probe: %w", err)
	}
	facts, err := ParseHostFacts(result.Stdout + "\n" + result.Stderr)
	if err != nil {
		return StandardDetectResult{}, err
	}
	if refusal := StandardRefusal(facts); refusal != "" {
		return StandardDetectResult{Facts: facts, Refusal: refusal}, nil
	}
	diagnosis := s.diagnosePrivilege(ctx, runner, facts, input.Password, input.SSHUser)
	return StandardDetectResult{Facts: facts, Diagnosis: diagnosis}, nil
}

// RootAssist applies a root-assisted sudo fix, then the caller should re-run detection.
func (s *HostService) RootAssist(ctx context.Context, input RootAssistInput) error {
	port := normalizePort(input.SSHPort)
	runner, err := remotessh.ConnectPassword(ctx, input.Hostname, port, "root", input.RootPassword)
	if err != nil {
		return fmt.Errorf("connect as root: %w", err)
	}
	defer func() { _ = runner.Close() }()

	manager := input.Facts.PackageManager
	if manager == "" {
		manager = pkgmgr.ManagerAPT
	}
	for _, command := range rootAssistCommands(manager, input.SSHUser, input.Diagnosis) {
		if _, err := runner.Run(ctx, command, nil); err != nil {
			return fmt.Errorf("apply root assist command %q: %w", command, err)
		}
	}
	return nil
}

// StartBootstrap starts a Standard bootstrap workflow and returns its stream id.
func (s *HostService) StartBootstrap(ctx context.Context, input BootstrapStartInput) (BootstrapSession, error) {
	sessionID, err := s.Streams.Create()
	if err != nil {
		return BootstrapSession{}, err
	}
	password := append([]byte{}, input.Password...)
	runCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.sessions[sessionID] = bootstrapSessionState{Input: input, Cancel: cancel}
	s.mu.Unlock()
	go s.runBootstrapSession(runCtx, sessionID, input, password)
	return BootstrapSession{ID: sessionID}, nil
}

// CancelBootstrap cancels an in-flight bootstrap session and triggers best-effort rollback.
func (s *HostService) CancelBootstrap(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	state, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("bootstrap session %s is not found", sessionID)
	}
	if state.Cancel != nil {
		state.Cancel()
	}
	return nil
}

// CommitBootstrap saves a completed bootstrap session as a host.
func (s *HostService) CommitBootstrap(ctx context.Context, input CommitHostInput) (Host, error) {
	s.mu.Lock()
	state, ok := s.sessions[input.SessionID]
	s.mu.Unlock()
	if !ok || !state.Complete {
		return Host{}, fmt.Errorf("bootstrap session %s is not complete", input.SessionID)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = state.Facts.Hostname
	}
	host, err := s.Hosts.Create(ctx, HostCreate{
		Name:            name,
		Hostname:        state.Input.Hostname,
		SSHPort:         normalizePort(state.Input.SSHPort),
		SSHUser:         "patchpilot",
		Type:            HostTypeStandard,
		PackageManager:  string(state.Facts.PackageManager),
		OSPretty:        state.Facts.OSPretty,
		Arch:            state.Facts.Arch,
		IsProxmoxHost:   state.Facts.IsProxmox,
		Enabled:         input.Enabled,
		AutoReboot:      input.AutoReboot,
		AutoRebootDelay: NormalizeRebootDelay(input.AutoRebootDelay),
		Notes:           input.Notes,
	})
	if err != nil {
		return Host{}, err
	}
	s.audit(ctx, "tui", "host.add", "host", &host.ID, map[string]any{"name": host.Name, "session_id": input.SessionID})
	return host, nil
}

// DryRunHost runs a package-manager dry-run through the installed patchpilot key.
func (s *HostService) DryRunHost(ctx context.Context, hostID int64) (pkgmgr.DryRunResult, error) {
	host, err := s.Hosts.Get(ctx, hostID)
	if err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	if host.Type == HostTypeTrueNAS || pkgmgr.PackageManager(host.PackageManager) == pkgmgr.ManagerTrueNASAPI {
		return s.dryRunTrueNAS(ctx, host)
	}
	if host.Type == HostTypeHAOS || pkgmgr.PackageManager(host.PackageManager) == pkgmgr.ManagerHAOSAPI {
		return s.dryRunHAOS(ctx, host)
	}
	return s.dryRunExistingHost(ctx, host)
}

// RebootHost asks a Standard host to reboot through its installed patchpilot SSH account.
func (s *HostService) RebootHost(ctx context.Context, hostID int64) error {
	host, err := s.Hosts.Get(ctx, hostID)
	if err != nil {
		return err
	}
	if host.Type != HostTypeStandard || host.IsSelf {
		return fmt.Errorf("reboot is only supported for standard non-self hosts")
	}
	keypair, err := s.Keys.Ensure(ctx)
	if err != nil {
		return err
	}
	runner, err := remotessh.ConnectKey(ctx, host.Hostname, normalizePort(host.SSHPort), host.SSHUser, keypair.Signer)
	if err != nil {
		return err
	}
	defer func() { _ = runner.Close() }()
	if _, err := runner.Run(ctx, "sudo -n systemctl reboot || sudo -n reboot", nil); err != nil {
		return fmt.Errorf("reboot host %s: %w", host.Name, err)
	}
	s.audit(ctx, "tui", "host.reboot", "host", &host.ID, map[string]any{"name": host.Name})
	return nil
}

type bootstrapSessionState struct {
	Input         BootstrapStartInput
	Facts         HostFacts
	DryRun        pkgmgr.DryRunResult
	ResolvedPaths map[string]string
	SudoersEntry  string
	Complete      bool
	Cancel        context.CancelFunc
}

type streamReporter struct {
	hub       *stream.Hub
	sessionID string
}

func (r streamReporter) Report(event BootstrapEvent) {
	_ = r.hub.Publish(r.sessionID, "bootstrap.step", event)
}

func (s *HostService) runBootstrapSession(ctx context.Context, sessionID string, input BootstrapStartInput, password []byte) {
	defer ZeroBytes(password)
	defer s.Streams.Complete(sessionID)
	reporter := streamReporter{hub: s.Streams, sessionID: sessionID}
	completed := false
	var privilegedRunner remotessh.Runner
	resolvedPaths := map[string]string{}
	defer func() {
		if privilegedRunner != nil {
			_ = privilegedRunner.Close()
		}
	}()
	defer func() {
		if completed || privilegedRunner == nil {
			return
		}
		RollbackBootstrap(context.Background(), privilegedRunner, input.Facts, password, input.Facts.PackageManager, resolvedPaths, reporter)
	}()
	reportError := func(step string, err error) {
		reporter.Report(BootstrapEvent{Step: step, Status: BootstrapStepFailed, Message: err.Error()})
		_ = s.Streams.Publish(sessionID, "bootstrap.error", map[string]string{"error": err.Error()})
	}

	keypair, err := s.Keys.Ensure(ctx)
	if err != nil {
		reportError("Loading orchestrator key", err)
		return
	}
	reporter.Report(BootstrapEvent{Step: "Connecting via SSH as " + input.SSHUser, Status: BootstrapStepRunning, Message: input.Hostname})
	runner, err := remotessh.ConnectPassword(ctx, input.Hostname, normalizePort(input.SSHPort), input.SSHUser, password)
	if err != nil {
		reportError("Connecting via SSH as "+input.SSHUser, err)
		return
	}
	privilegedRunner = runner
	reporter.Report(BootstrapEvent{Step: "Connecting via SSH as " + input.SSHUser, Status: BootstrapStepSuccess, Message: "connected"})

	sourceIP, err := ResolveSourceIP(ctx, input.Hostname, normalizePort(input.SSHPort))
	if err != nil {
		sourceIP = ""
	}
	bootstrapResult, err := BootstrapStandardWithRunner(ctx, runner, BootstrapOptions{
		Facts:        input.Facts,
		Manager:      input.Facts.PackageManager,
		Password:     password,
		PublicKey:    keypair.PublicKey,
		SourceIP:     sourceIP,
		OriginalUser: input.SSHUser,
	}, reporter)
	if err != nil {
		reportError("Bootstrap failed", err)
		return
	}
	resolvedPaths = bootstrapResult.ResolvedPaths

	reporter.Report(BootstrapEvent{Step: "Reconnecting as patchpilot with key", Status: BootstrapStepRunning, Message: "verifying new login path"})
	patchpilotRunner, err := remotessh.ConnectKey(ctx, input.Hostname, normalizePort(input.SSHPort), "patchpilot", keypair.Signer)
	if err != nil {
		reportError("Reconnecting as patchpilot with key", err)
		return
	}
	defer func() { _ = patchpilotRunner.Close() }()
	reporter.Report(BootstrapEvent{Step: "Reconnecting as patchpilot with key", Status: BootstrapStepSuccess, Message: "connected"})

	verifyCommand, err := pkgmgr.VerifyCommand(input.Facts.PackageManager, bootstrapResult.ResolvedPaths)
	if err != nil {
		reportError("Verifying NOPASSWD sudo", err)
		return
	}
	if err := runStep(ctx, patchpilotRunner, reporter, "Verifying NOPASSWD sudo", "Running package-manager sudo probe", verifyCommand, nil); err != nil {
		reportError("Verifying NOPASSWD sudo", err)
		return
	}

	dryRun, err := s.dryRunWithRunner(ctx, patchpilotRunner, input.Facts.PackageManager, bootstrapResult.ResolvedPaths)
	if err != nil {
		reportError("Dry run", err)
		return
	}
	reporter.Report(BootstrapEvent{Step: "Wiping your password from memory", Status: BootstrapStepSuccess, Message: "password buffer zeroed after this session completes"})

	summary := BootstrapSummary{Facts: input.Facts, DryRun: dryRun, ResolvedPaths: bootstrapResult.ResolvedPaths, SudoersEntry: bootstrapResult.SudoersEntry}
	s.mu.Lock()
	state := s.sessions[sessionID]
	state.Input = input
	state.Facts = input.Facts
	state.DryRun = dryRun
	state.ResolvedPaths = bootstrapResult.ResolvedPaths
	state.SudoersEntry = bootstrapResult.SudoersEntry
	state.Complete = true
	s.sessions[sessionID] = state
	s.mu.Unlock()
	completed = true
	_ = s.Streams.Publish(sessionID, "bootstrap.complete", summary)
}

func (s *HostService) diagnosePrivilege(ctx context.Context, runner remotessh.Runner, facts HostFacts, password []byte, user string) *PermissionDiagnosis {
	if facts.CurrentUID == "0" || facts.SudoNoPassword {
		return nil
	}
	if !facts.SudoPresent {
		return &PermissionDiagnosis{Kind: "sudo_missing", Reason: "sudo is not installed", PathForward: "Provide the root password so patchpilot can install sudo and add the user to the admin group.", CanAutoFix: true, PreinstallOK: true, ManualFix: manualFix(facts.PackageManager, user, true)}
	}
	command, input := escalate(facts, "true", password)
	if _, err := runner.Run(ctx, command, input); err == nil {
		return nil
	}
	return &PermissionDiagnosis{Kind: "sudo_denied", Reason: "the SSH user cannot run sudo with the supplied password", PathForward: "Provide the root password so patchpilot can add the user to the admin group, or use pre-install mode.", CanAutoFix: true, PreinstallOK: true, ManualFix: manualFix(facts.PackageManager, user, false)}
}

func (s *HostService) dryRunExistingHost(ctx context.Context, host Host) (pkgmgr.DryRunResult, error) {
	keypair, err := s.Keys.Ensure(ctx)
	if err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	runner, err := remotessh.ConnectKey(ctx, host.Hostname, host.SSHPort, host.SSHUser, keypair.Signer)
	if err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	defer func() { _ = runner.Close() }()
	manager := pkgmgr.PackageManager(host.PackageManager)
	paths, err := resolveCommandPaths(ctx, runner, nil, HostFacts{CurrentUID: "0", PackageManager: manager}, manager, nil)
	if err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	return s.dryRunWithRunner(ctx, runner, manager, paths)
}

func (s *HostService) dryRunWithRunner(ctx context.Context, runner remotessh.Runner, manager pkgmgr.PackageManager, paths map[string]string) (pkgmgr.DryRunResult, error) {
	command, err := pkgmgr.DryRunCommand(manager, paths)
	if err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	result, err := runner.Run(ctx, command, nil)
	if err != nil && (manager != pkgmgr.ManagerDNF || result.ExitCode != 100) {
		return pkgmgr.DryRunResult{}, fmt.Errorf("run dry-run command: %w", err)
	}
	return pkgmgr.ParseDryRunOutput(manager, result.Stdout+"\n"+result.Stderr, result.ExitCode)
}

func normalizePort(port int) int {
	if port == 0 {
		return 22
	}
	return port
}

func rootAssistCommands(manager pkgmgr.PackageManager, user string, diagnosis *PermissionDiagnosis) []string {
	var commands []string
	if diagnosis != nil && diagnosis.Kind == "sudo_missing" {
		switch manager {
		case pkgmgr.ManagerDNF:
			commands = append(commands, "dnf install -y sudo")
		case pkgmgr.ManagerPacman:
			commands = append(commands, "pacman -Sy --noconfirm sudo")
		case pkgmgr.ManagerZypper:
			commands = append(commands, "zypper -n install sudo")
		case pkgmgr.ManagerAPK:
			commands = append(commands, "apk add sudo")
		default:
			commands = append(commands, "apt-get update && apt-get install -y sudo")
		}
	}
	group := "sudo"
	if manager == pkgmgr.ManagerDNF || manager == pkgmgr.ManagerPacman || manager == pkgmgr.ManagerZypper {
		group = "wheel"
	}
	if manager == pkgmgr.ManagerAPK {
		commands = append(commands, fmt.Sprintf("adduser %s wheel", shellQuote(user)))
		return commands
	}
	commands = append(commands, fmt.Sprintf("usermod -aG %s %s", group, shellQuote(user)))
	return commands
}

func manualFix(manager pkgmgr.PackageManager, user string, installSudo bool) []string {
	var commands []string
	if installSudo {
		switch manager {
		case pkgmgr.ManagerDNF:
			commands = append(commands, "dnf install -y sudo")
		case pkgmgr.ManagerPacman:
			commands = append(commands, "pacman -Sy --noconfirm sudo")
		case pkgmgr.ManagerZypper:
			commands = append(commands, "zypper -n install sudo")
		case pkgmgr.ManagerAPK:
			commands = append(commands, "apk add sudo")
		default:
			commands = append(commands, "apt-get install -y sudo")
		}
	}
	if manager == pkgmgr.ManagerAPK {
		commands = append(commands, "adduser "+user+" wheel")
		return commands
	}
	group := "sudo"
	if manager == pkgmgr.ManagerDNF || manager == pkgmgr.ManagerPacman || manager == pkgmgr.ManagerZypper {
		group = "wheel"
	}
	commands = append(commands, "usermod -aG "+group+" "+user)
	return commands
}

func (s *HostService) audit(ctx context.Context, actor string, action string, targetType string, targetID *int64, details map[string]any) {
	if s.Audit == nil {
		return
	}
	payload, err := json.Marshal(details)
	if err != nil {
		payload = []byte("{}")
	}
	if err := s.Audit.Create(ctx, AuditEvent{Actor: actor, Action: action, TargetType: targetType, TargetID: targetID, Details: string(payload)}); err != nil && s.Logger != nil {
		s.Logger.Error("create audit event", "error", err)
	}
}

func stripANSI(value string) string {
	var builder strings.Builder
	inEscape := false
	for index := 0; index < len(value); index++ {
		char := value[index]
		if inEscape {
			if char >= '@' && char <= '~' {
				inEscape = false
			}
			continue
		}
		if char == 0x1b {
			inEscape = true
			continue
		}
		builder.WriteByte(char)
	}
	return builder.String()
}
