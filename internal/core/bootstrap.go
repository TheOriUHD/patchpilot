package core

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/TheOriUHD/patchpilot/internal/pkgmgr"
	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
)

// BootstrapStepStatus is the status of a wizard progress step.
type BootstrapStepStatus string

const (
	// BootstrapStepRunning means the step is in progress.
	BootstrapStepRunning BootstrapStepStatus = "running"
	// BootstrapStepSuccess means the step completed successfully.
	BootstrapStepSuccess BootstrapStepStatus = "success"
	// BootstrapStepFailed means the step failed.
	BootstrapStepFailed BootstrapStepStatus = "failed"
)

// BootstrapEvent is a progress event emitted during Standard host onboarding.
type BootstrapEvent struct {
	Step    string              `json:"step"`
	Status  BootstrapStepStatus `json:"status"`
	Message string              `json:"message"`
}

// BootstrapReporter receives bootstrap progress events.
type BootstrapReporter interface {
	Report(event BootstrapEvent)
}

// BootstrapOptions contains the data needed to bootstrap a Standard host.
type BootstrapOptions struct {
	Facts        HostFacts
	Manager      pkgmgr.PackageManager
	Password     []byte
	PublicKey    string
	SourceIP     string
	OriginalUser string
}

// BootstrapResult contains resolved target details discovered during bootstrap.
type BootstrapResult struct {
	ResolvedPaths map[string]string
	SudoersEntry  string
}

// BootstrapCommands is the shell command set used during bootstrap.
type BootstrapCommands struct {
	CreateUser         string
	CreateSSHDir       string
	InstallPublicKey   string
	WriteSudoers       string
	ValidateSudoers    string
	RollbackSudoers    string
	RollbackKey        string
	RollbackUser       string
	VerifySudoNoPasswd string
}

// GenerateBootstrapCommands returns the concrete shell commands for target bootstrap.
func GenerateBootstrapCommands(publicKey string, sourceIP string, manager pkgmgr.PackageManager, resolvedPaths map[string]string) (BootstrapCommands, error) {
	sudoersEntry, err := pkgmgr.SudoersEntry("patchpilot", manager, resolvedPaths)
	if err != nil {
		return BootstrapCommands{}, err
	}
	verify, err := pkgmgr.VerifyCommand(manager, resolvedPaths)
	if err != nil {
		return BootstrapCommands{}, err
	}

	authorizedKey := hardenedAuthorizedKey(publicKey, sourceIP)
	createUser := "id patchpilot >/dev/null 2>&1 || useradd --system --create-home --shell /bin/sh patchpilot"
	if manager == pkgmgr.ManagerAPK {
		createUser = "id patchpilot >/dev/null 2>&1 || adduser -D -h /home/patchpilot -s /bin/sh patchpilot"
	}

	return BootstrapCommands{
		CreateUser:         createUser,
		CreateSSHDir:       "install -d -m 0700 -o patchpilot -g patchpilot /home/patchpilot/.ssh",
		InstallPublicKey:   "printf %s " + shellQuote(authorizedKey) + " > /home/patchpilot/.ssh/authorized_keys && chown patchpilot:patchpilot /home/patchpilot/.ssh/authorized_keys && chmod 0600 /home/patchpilot/.ssh/authorized_keys",
		WriteSudoers:       "cat > /etc/sudoers.d/patchpilot <<'PATCHPILOT_SUDOERS'\n" + sudoersEntry + "PATCHPILOT_SUDOERS\nchmod 0440 /etc/sudoers.d/patchpilot",
		ValidateSudoers:    "visudo -cf /etc/sudoers.d/patchpilot",
		RollbackSudoers:    "rm -f /etc/sudoers.d/patchpilot",
		RollbackKey:        "rm -f /home/patchpilot/.ssh/authorized_keys",
		RollbackUser:       "userdel -r patchpilot >/dev/null 2>&1 || deluser --remove-home patchpilot >/dev/null 2>&1 || true",
		VerifySudoNoPasswd: verify,
	}, nil
}

// RollbackBootstrap removes bootstrap artifacts that may have been created before failure or cancellation.
func RollbackBootstrap(ctx context.Context, runner remotessh.Runner, facts HostFacts, password []byte, manager pkgmgr.PackageManager, resolvedPaths map[string]string, reporter BootstrapReporter) {
	reporter = nonNilReporter(reporter)
	commands, err := GenerateBootstrapCommands("ssh-ed25519 AAAA rollback-only", "", manager, resolvedPaths)
	if err != nil {
		commands = BootstrapCommands{RollbackSudoers: "rm -f /etc/sudoers.d/patchpilot", RollbackKey: "rm -f /home/patchpilot/.ssh/authorized_keys", RollbackUser: "userdel -r patchpilot >/dev/null 2>&1 || deluser --remove-home patchpilot >/dev/null 2>&1 || true"}
	}
	reporter.Report(BootstrapEvent{Step: "Rollback", Status: BootstrapStepRunning, Message: "Removing bootstrap artifacts"})
	rollback := []string{commands.RollbackSudoers, commands.RollbackKey}
	if !facts.PatchpilotUserExists {
		rollback = append(rollback, commands.RollbackUser)
	}
	for _, command := range rollback {
		escalated, input := escalate(facts, command, password)
		_, _ = runner.Run(ctx, escalated, input)
	}
	reporter.Report(BootstrapEvent{Step: "Rollback", Status: BootstrapStepSuccess, Message: "Best-effort rollback complete"})
}

// BootstrapStandardWithRunner bootstraps the patchpilot user over an already-open privileged session.
func BootstrapStandardWithRunner(ctx context.Context, runner remotessh.Runner, options BootstrapOptions, reporter BootstrapReporter) (BootstrapResult, error) {
	reporter = nonNilReporter(reporter)
	manager := options.Manager
	if manager == "" {
		manager = options.Facts.PackageManager
	}

	if err := runEscalatedStep(ctx, runner, reporter, options.Facts, options.Password, "Privilege check", "Verifying sudo/root access", "true"); err != nil {
		return BootstrapResult{}, err
	}

	paths, err := resolveCommandPaths(ctx, runner, reporter, options.Facts, manager, options.Password)
	if err != nil {
		return BootstrapResult{}, err
	}
	commands, err := GenerateBootstrapCommands(options.PublicKey, options.SourceIP, manager, paths)
	if err != nil {
		return BootstrapResult{}, err
	}

	if err := runEscalatedStep(ctx, runner, reporter, options.Facts, options.Password, "Creating system user 'patchpilot'", "Creating or reusing patchpilot user", commands.CreateUser); err != nil {
		return BootstrapResult{}, err
	}
	if err := runEscalatedStep(ctx, runner, reporter, options.Facts, options.Password, "Creating /home/patchpilot/.ssh", "Preparing SSH directory", commands.CreateSSHDir); err != nil {
		return BootstrapResult{}, err
	}
	if err := runEscalatedStep(ctx, runner, reporter, options.Facts, options.Password, "Installing orchestrator public key", "Writing authorized_keys", commands.InstallPublicKey); err != nil {
		return BootstrapResult{}, err
	}
	if err := runEscalatedStep(ctx, runner, reporter, options.Facts, options.Password, "Writing /etc/sudoers.d/patchpilot", "Writing sudoers policy", commands.WriteSudoers); err != nil {
		return BootstrapResult{}, err
	}

	validateCommand, validateInput := escalate(options.Facts, commands.ValidateSudoers, options.Password)
	reporter.Report(BootstrapEvent{Step: "Validating sudoers with visudo -cf", Status: BootstrapStepRunning, Message: "Checking sudoers syntax before closing privileged session"})
	if result, err := runner.Run(ctx, validateCommand, validateInput); err != nil {
		rollbackCommand, rollbackInput := escalate(options.Facts, commands.RollbackSudoers, options.Password)
		_, rollbackErr := runner.Run(ctx, rollbackCommand, rollbackInput)
		message := strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
		if rollbackErr != nil {
			message = message + "; rollback failed: " + rollbackErr.Error()
		}
		reporter.Report(BootstrapEvent{Step: "Validating sudoers with visudo -cf", Status: BootstrapStepFailed, Message: message})
		return BootstrapResult{}, fmt.Errorf("validate sudoers and rollback invalid file: %w", err)
	}
	reporter.Report(BootstrapEvent{Step: "Validating sudoers with visudo -cf", Status: BootstrapStepSuccess, Message: "sudoers syntax is valid"})

	return BootstrapResult{ResolvedPaths: paths, SudoersEntry: mustSudoers(manager, paths)}, nil
}

// ResolveSourceIP returns the local IP address used to reach the target host.
func ResolveSourceIP(ctx context.Context, hostname string, port int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	conn, err := net.Dial("udp", net.JoinHostPort(hostname, strconv.Itoa(port)))
	if err != nil {
		return "", fmt.Errorf("resolve source ip: %w", err)
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return "", fmt.Errorf("resolve source ip: unexpected local address %s", conn.LocalAddr())
	}
	return addr.IP.String(), nil
}

func resolveCommandPaths(ctx context.Context, runner remotessh.Runner, reporter BootstrapReporter, facts HostFacts, manager pkgmgr.PackageManager, password []byte) (map[string]string, error) {
	reporter = nonNilReporter(reporter)
	required, err := pkgmgr.RequiredCommands(manager)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]string, len(required))
	for _, name := range required {
		step := "Resolving command path: " + name
		reporter.Report(BootstrapEvent{Step: step, Status: BootstrapStepRunning, Message: "Running command -v " + name})
		command, input := escalate(facts, "command -v "+shellQuote(name), password)
		result, err := runner.Run(ctx, command, input)
		if err != nil {
			reporter.Report(BootstrapEvent{Step: step, Status: BootstrapStepFailed, Message: strings.TrimSpace(result.Stderr + result.Stdout)})
			return nil, fmt.Errorf("resolve command path %s: %w", name, err)
		}
		path := firstLine(result.Stdout)
		if !strings.HasPrefix(path, "/") {
			reporter.Report(BootstrapEvent{Step: step, Status: BootstrapStepFailed, Message: "command did not resolve to an absolute path"})
			return nil, fmt.Errorf("command %s resolved to non-absolute path %q", name, path)
		}
		paths[name] = path
		reporter.Report(BootstrapEvent{Step: step, Status: BootstrapStepSuccess, Message: path})
	}
	return paths, nil
}

func runStep(ctx context.Context, runner remotessh.Runner, reporter BootstrapReporter, step string, message string, command string, input []byte) error {
	reporter.Report(BootstrapEvent{Step: step, Status: BootstrapStepRunning, Message: message})
	result, err := runner.Run(ctx, command, input)
	if err != nil {
		failure := strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
		if failure == "" {
			failure = err.Error()
		}
		reporter.Report(BootstrapEvent{Step: step, Status: BootstrapStepFailed, Message: failure})
		return fmt.Errorf("%s: %w", step, err)
	}
	reporter.Report(BootstrapEvent{Step: step, Status: BootstrapStepSuccess, Message: strings.TrimSpace(result.Stdout)})
	return nil
}

func runEscalatedStep(ctx context.Context, runner remotessh.Runner, reporter BootstrapReporter, facts HostFacts, password []byte, step string, message string, command string) error {
	escalatedCommand, input := escalate(facts, command, password)
	return runStep(ctx, runner, reporter, step, message, escalatedCommand, input)
}

func escalate(facts HostFacts, command string, password []byte) (string, []byte) {
	if facts.CurrentUID == "0" {
		return "sh -c " + shellQuote(command), nil
	}
	if facts.SudoNoPassword {
		return "sudo -n sh -c " + shellQuote(command), nil
	}
	input := append([]byte{}, password...)
	input = append(input, '\n')
	return "sudo -S -p '' sh -c " + shellQuote(command), input
}

func hardenedAuthorizedKey(publicKey string, sourceIP string) string {
	trimmed := strings.TrimSpace(publicKey)
	if sourceIP == "" {
		return "no-pty,no-X11-forwarding,no-agent-forwarding,no-port-forwarding " + trimmed + "\n"
	}
	return "from=\"" + sourceIP + "\",no-pty,no-X11-forwarding,no-agent-forwarding,no-port-forwarding " + trimmed + "\n"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func mustSudoers(manager pkgmgr.PackageManager, paths map[string]string) string {
	entry, err := pkgmgr.SudoersEntry("patchpilot", manager, paths)
	if err != nil {
		return ""
	}
	return entry
}

type discardReporter struct{}

func nonNilReporter(reporter BootstrapReporter) BootstrapReporter {
	if reporter == nil {
		return discardReporter{}
	}
	return reporter
}

func (discardReporter) Report(BootstrapEvent) {}
