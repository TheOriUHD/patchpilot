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
	reportBootstrapEvent(reporter, BootstrapEvent{Step: "Rollback", Status: BootstrapStepRunning, Message: "Removing bootstrap artifacts"})
	rollback := []string{commands.RollbackSudoers, commands.RollbackKey}
	if !facts.PatchpilotUserExists {
		rollback = append(rollback, commands.RollbackUser)
	}
	for _, command := range rollback {
		escalated, input := escalate(facts, command, password)
		_, _ = runner.Run(ctx, escalated, input)
	}
	reportBootstrapEvent(reporter, BootstrapEvent{Step: "Rollback", Status: BootstrapStepSuccess, Message: "Best-effort rollback complete"})
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
	reportBootstrapEvent(reporter, BootstrapEvent{Step: "Validating sudoers with visudo -cf", Status: BootstrapStepRunning, Message: "Checking sudoers syntax before closing privileged session"})
	if result, err := runner.Run(ctx, validateCommand, validateInput); err != nil {
		rollbackCommand, rollbackInput := escalate(options.Facts, commands.RollbackSudoers, options.Password)
		_, rollbackErr := runner.Run(ctx, rollbackCommand, rollbackInput)
		message := strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
		if rollbackErr != nil {
			message = message + "; rollback failed: " + rollbackErr.Error()
		}
		reportBootstrapEvent(reporter, BootstrapEvent{Step: "Validating sudoers with visudo -cf", Status: BootstrapStepFailed, Message: message})
		return BootstrapResult{}, fmt.Errorf("validate sudoers and rollback invalid file: %w", err)
	}
	reportBootstrapEvent(reporter, BootstrapEvent{Step: "Validating sudoers with visudo -cf", Status: BootstrapStepSuccess, Message: "sudoers syntax is valid"})

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
	defer func() { _ = conn.Close() }()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return "", fmt.Errorf("resolve source ip: unexpected local address %s", conn.LocalAddr())
	}
	return addr.IP.String(), nil
}

func resolveCommandPaths(ctx context.Context, runner remotessh.Runner, reporter BootstrapReporter, facts HostFacts, manager pkgmgr.PackageManager, password []byte) (map[string]string, error) {
	required, err := pkgmgr.RequiredCommands(manager)
	if err != nil {
		return nil, err
	}
	return resolveRequiredCommandPaths(ctx, runner, reporter, facts, password, required)
}

func resolveRequiredCommandPaths(ctx context.Context, runner remotessh.Runner, reporter BootstrapReporter, facts HostFacts, password []byte, required []string) (map[string]string, error) {
	reporter = nonNilReporter(reporter)
	for _, name := range required {
		reportBootstrapEvent(reporter, BootstrapEvent{Step: commandPathStep(name), Status: BootstrapStepRunning, Message: "Resolving with command -v -p, which, and canonical paths"})
	}

	command, input := escalate(facts, commandPathResolutionScript(required), password)
	result, runErr := runner.Run(ctx, command, input)
	paths, parseErr := parseResolvedCommandPaths(result.Stdout, required)
	if runErr != nil {
		missing := missingCommandFromResolverOutput(result.Stderr)
		if missing == "" && parseErr != nil {
			missing = firstMissingRequired(required, paths)
		}
		message := strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
		if missing != "" {
			message = fmt.Sprintf("command %s could not be resolved to an executable absolute path", missing)
			reportBootstrapEvent(reporter, BootstrapEvent{Step: commandPathStep(missing), Status: BootstrapStepFailed, Message: message})
			return nil, fmt.Errorf("resolve command path %s: %s", missing, message)
		}
		if message == "" {
			message = runErr.Error()
		}
		reportBootstrapEvent(reporter, BootstrapEvent{Step: "Resolving command paths", Status: BootstrapStepFailed, Message: message})
		return nil, fmt.Errorf("resolve command paths: %w", runErr)
	}
	if parseErr != nil {
		missing := firstMissingRequired(required, paths)
		if missing != "" {
			reportBootstrapEvent(reporter, BootstrapEvent{Step: commandPathStep(missing), Status: BootstrapStepFailed, Message: parseErr.Error()})
		} else {
			reportBootstrapEvent(reporter, BootstrapEvent{Step: "Resolving command paths", Status: BootstrapStepFailed, Message: parseErr.Error()})
		}
		return nil, parseErr
	}
	for _, name := range required {
		reportBootstrapEvent(reporter, BootstrapEvent{Step: commandPathStep(name), Status: BootstrapStepSuccess, Message: paths[name]})
	}
	return paths, nil
}

func commandPathResolutionScript(required []string) string {
	var builder strings.Builder
	builder.WriteString(`resolve_patchpilot_command() {
	name=$1
	resolved=
	candidate=$(command -v -p "$name" 2>/dev/null || true)
	case "$candidate" in
		/*) resolved=$candidate ;;
	esac
	if [ -z "$resolved" ]; then
		candidate=$(which "$name" 2>/dev/null || true)
		case "$candidate" in
			/*) resolved=$candidate ;;
		esac
	fi
	if [ -z "$resolved" ]; then
		for prefix in /usr/bin /bin /usr/sbin /sbin; do
			candidate=$prefix/$name
			if test -x "$candidate"; then
				resolved=$candidate
				break
			fi
		done
	fi
	if [ -z "$resolved" ]; then
		printf 'missing command: %s\n' "$name" >&2
		return 1
	fi
	printf '%s\t%s\n' "$name" "$resolved"
}
status=0
`)
	for _, name := range required {
		builder.WriteString("resolve_patchpilot_command ")
		builder.WriteString(shellQuote(name))
		builder.WriteString(" || status=1\n")
	}
	builder.WriteString("exit \"$status\"\n")
	return builder.String()
}

func parseResolvedCommandPaths(output string, required []string) (map[string]string, error) {
	paths := make(map[string]string, len(required))
	requiredSet := make(map[string]struct{}, len(required))
	for _, name := range required {
		requiredSet[name] = struct{}{}
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, path, ok := strings.Cut(line, "\t")
		if !ok {
			return paths, fmt.Errorf("parse resolved command path line %q", line)
		}
		name = strings.TrimSpace(name)
		path = strings.TrimSpace(path)
		if _, ok := requiredSet[name]; !ok {
			continue
		}
		if !strings.HasPrefix(path, "/") {
			return paths, fmt.Errorf("command %s resolved to non-absolute path %q", name, path)
		}
		paths[name] = path
	}
	if missing := firstMissingRequired(required, paths); missing != "" {
		return paths, fmt.Errorf("command %s could not be resolved to an executable absolute path", missing)
	}
	return paths, nil
}

func missingCommandFromResolverOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "missing command: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "missing command: "))
		}
	}
	return ""
}

func firstMissingRequired(required []string, paths map[string]string) string {
	for _, name := range required {
		if paths[name] == "" {
			return name
		}
	}
	return ""
}

func commandPathStep(name string) string {
	return "Resolving command path: " + name
}

func runStep(ctx context.Context, runner remotessh.Runner, reporter BootstrapReporter, step string, message string, command string, input []byte) error {
	reportBootstrapEvent(reporter, BootstrapEvent{Step: step, Status: BootstrapStepRunning, Message: message})
	result, err := runner.Run(ctx, command, input)
	if err != nil {
		failure := strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
		if failure == "" {
			failure = err.Error()
		}
		reportBootstrapEvent(reporter, BootstrapEvent{Step: step, Status: BootstrapStepFailed, Message: failure})
		return fmt.Errorf("%s: %w", step, err)
	}
	reportBootstrapEvent(reporter, BootstrapEvent{Step: step, Status: BootstrapStepSuccess, Message: strings.TrimSpace(result.Stdout)})
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

func mustSudoers(manager pkgmgr.PackageManager, paths map[string]string) string {
	entry, err := pkgmgr.SudoersEntry("patchpilot", manager, paths)
	if err != nil {
		return ""
	}
	return entry
}

func reportBootstrapEvent(reporter BootstrapReporter, event BootstrapEvent) {
	bootstrapDebugf("bootstrap.go: before Report step=%q status=%q message=%q", event.Step, event.Status, event.Message)
	reporter.Report(event)
	bootstrapDebugf("bootstrap.go: after Report step=%q status=%q", event.Step, event.Status)
}

type discardReporter struct{}

func nonNilReporter(reporter BootstrapReporter) BootstrapReporter {
	if reporter == nil {
		return discardReporter{}
	}
	return reporter
}

func (discardReporter) Report(BootstrapEvent) {}
