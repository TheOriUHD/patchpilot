package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/TheOriUHD/patchpilot/internal/pkgmgr"
	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
)

func TestBootstrapRollsBackInvalidSudoersBeforeReturning(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: aptResolvedPathsOutput()}},
		"visudo -cf":                 {result: remotessh.CommandResult{Stderr: "parse error"}, err: errors.New("exit status 1")},
	}}
	_, err := BootstrapStandardWithRunner(context.Background(), runner, BootstrapOptions{
		Facts:     HostFacts{CurrentUID: "0", PackageManager: pkgmgr.ManagerAPT},
		Manager:   pkgmgr.ManagerAPT,
		PublicKey: "ssh-ed25519 AAAA patchpilot",
		SourceIP:  "192.0.2.10",
	}, nil)
	if err == nil {
		t.Fatal("BootstrapStandardWithRunner() expected visudo failure")
	}

	validateIndex := runner.indexContaining("visudo -cf /etc/sudoers.d/patchpilot")
	rollbackIndex := runner.indexContaining("rm -f /etc/sudoers.d/patchpilot")
	if validateIndex == -1 || rollbackIndex == -1 {
		t.Fatalf("commands = %#v; want validate and rollback", runner.commands)
	}
	if rollbackIndex <= validateIndex {
		t.Fatalf("rollback index %d must be after validate index %d", rollbackIndex, validateIndex)
	}
	if runner.closed {
		t.Fatal("bootstrapper must not close privileged runner before caller can inspect/recover")
	}
}

func TestResolveRequiredCommandPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		required []string
		stdout   string
		stderr   string
		runErr   error
		want     map[string]string
		wantErr  string
	}{
		{
			name:     "normal apt-get",
			required: []string{"apt-get"},
			stdout:   "apt-get\t/usr/bin/apt-get\n",
			want:     map[string]string{"apt-get": "/usr/bin/apt-get"},
		},
		{
			name:     "builtin masking test",
			required: []string{"test"},
			stdout:   "test\t/usr/bin/test\n",
			want:     map[string]string{"test": "/usr/bin/test"},
		},
		{
			name:     "missing command",
			required: []string{"missing-command"},
			stderr:   "missing command: missing-command\n",
			runErr:   errors.New("exit status 1"),
			wantErr:  "missing-command",
		},
		{
			name:     "command in sbin",
			required: []string{"reboot"},
			stdout:   "reboot\t/sbin/reboot\n",
			want:     map[string]string{"reboot": "/sbin/reboot"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &fakeRunner{responses: map[string]fakeResponse{
				"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: tt.stdout, Stderr: tt.stderr}, err: tt.runErr},
			}}

			paths, err := resolveRequiredCommandPaths(context.Background(), runner, nil, HostFacts{CurrentUID: "0"}, nil, tt.required)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("resolveRequiredCommandPaths() expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveRequiredCommandPaths() error: %v", err)
			}
			for command, wantPath := range tt.want {
				if paths[command] != wantPath {
					t.Fatalf("paths[%q] = %q, want %q", command, paths[command], wantPath)
				}
			}
			if len(runner.commands) != 1 {
				t.Fatalf("commands = %#v, want one SSH round trip", runner.commands)
			}
			resolver := runner.commands[0]
			for _, needle := range []string{"command -v -p", "which", "/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
				if !strings.Contains(resolver, needle) {
					t.Fatalf("resolver command = %q, want it to contain %q", resolver, needle)
				}
			}
		})
	}
}

func TestGenerateBootstrapCommandsUsesResolvedPaths(t *testing.T) {
	t.Parallel()

	commands, err := GenerateBootstrapCommands("ssh-ed25519 AAAA patchpilot", "192.0.2.10", pkgmgr.ManagerAPT, map[string]string{
		"apt-get":   "/opt/bin/apt-get",
		"apt":       "/opt/bin/apt",
		"dpkg":      "/opt/bin/dpkg",
		"test":      "/bin/test",
		"systemctl": "/usr/bin/systemctl",
		"reboot":    "/usr/sbin/reboot",
		"shutdown":  "/usr/sbin/shutdown",
	})
	if err != nil {
		t.Fatalf("GenerateBootstrapCommands() error: %v", err)
	}
	if !strings.Contains(commands.WriteSudoers, "/opt/bin/apt-get") {
		t.Fatalf("WriteSudoers = %q, want resolved apt-get path", commands.WriteSudoers)
	}
	if !strings.Contains(commands.InstallPublicKey, `from="192.0.2.10"`) {
		t.Fatalf("InstallPublicKey = %q, want hardened from option", commands.InstallPublicKey)
	}
	for _, want := range []string{"/usr/bin/systemctl reboot", "/usr/sbin/reboot", "/usr/sbin/shutdown -r now"} {
		if !strings.Contains(commands.WriteSudoers, want) {
			t.Fatalf("WriteSudoers = %q, want reboot entry %q", commands.WriteSudoers, want)
		}
	}
}

func TestBuildEscalatedCommandSuUsesPTYAndStdin(t *testing.T) {
	t.Parallel()

	escalated, err := buildEscalatedCommand(escalationInput{Facts: HostFacts{CurrentUID: "1000", SuPresent: true}, RootPassword: []byte("root-secret")}, "id")
	if err != nil {
		t.Fatalf("buildEscalatedCommand() error: %v", err)
	}
	if escalated.Mode != escalationSuRoot {
		t.Fatalf("Mode = %q, want %q", escalated.Mode, escalationSuRoot)
	}
	if !escalated.Options.PTY {
		t.Fatal("su escalation must request a PTY")
	}
	if got := string(escalated.Stdin); got != "root-secret" {
		t.Fatalf("stdin = %q, want raw root password for PTY writer", got)
	}
	if strings.Contains(escalated.Command, "root-secret") {
		t.Fatalf("root password leaked into command: %q", escalated.Command)
	}
	if !strings.Contains(escalated.Command, "su -c") {
		t.Fatalf("command = %q, want su wrapper", escalated.Command)
	}
}

func TestSuBootstrapBatchIsIdempotentAndInstallsSudoWhenMissing(t *testing.T) {
	t.Parallel()

	commands, err := GenerateBootstrapCommands("ssh-ed25519 AAAA patchpilot", "", pkgmgr.ManagerAPT, map[string]string{
		"apt-get":   "/usr/bin/apt-get",
		"apt":       "/usr/bin/apt",
		"dpkg":      "/usr/bin/dpkg",
		"test":      "/usr/bin/test",
		"systemctl": "/usr/bin/systemctl",
		"reboot":    "/usr/sbin/reboot",
		"shutdown":  "/usr/sbin/shutdown",
		"id":        "/usr/bin/id",
		"install":   "/usr/bin/install",
		"chown":     "/usr/bin/chown",
		"chmod":     "/usr/bin/chmod",
		"useradd":   "/usr/sbin/useradd",
		"visudo":    "/usr/sbin/visudo",
	})
	if err != nil {
		t.Fatalf("GenerateBootstrapCommands() error: %v", err)
	}
	batch, err := suBootstrapBatch(pkgmgr.ManagerAPT, commands, true)
	if err != nil {
		t.Fatalf("suBootstrapBatch() error: %v", err)
	}
	if !strings.HasPrefix(batch, "export PATH=/usr/sbin:/usr/bin:/sbin:/bin:$PATH\n") {
		t.Fatalf("batch must begin with PATH export:\n%s", batch)
	}
	for _, want := range []string{
		"apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y sudo",
		"/usr/bin/id patchpilot >/dev/null 2>&1 || /usr/sbin/useradd",
		"/usr/bin/install -d -m 0700 -o patchpilot -g patchpilot /home/patchpilot/.ssh",
		"> /home/patchpilot/.ssh/authorized_keys",
		"cat > /etc/sudoers.d/patchpilot",
		"/usr/sbin/visudo -cf /etc/sudoers.d/patchpilot",
	} {
		if !strings.Contains(batch, want) {
			t.Fatalf("batch missing %q:\n%s", want, batch)
		}
	}
}

func TestBootstrapAdminCommandsIncludesRequestedTools(t *testing.T) {
	t.Parallel()

	commands := bootstrapAdminCommands(pkgmgr.ManagerAPT)
	for _, want := range []string{"useradd", "usermod", "groupadd", "visudo"} {
		if !containsString(commands, want) {
			t.Fatalf("bootstrapAdminCommands() = %#v, missing %q", commands, want)
		}
	}
}

func TestBootstrapStandardWithRunnerUsesSuBatch(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: aptResolvedPathsOutput()}},
	}}
	_, err := BootstrapStandardWithRunner(context.Background(), runner, BootstrapOptions{
		Facts:        HostFacts{CurrentUID: "1000", PackageManager: pkgmgr.ManagerAPT, SuPresent: true},
		Manager:      pkgmgr.ManagerAPT,
		Password:     []byte("user-secret"),
		RootPassword: []byte("root-secret"),
		PublicKey:    "ssh-ed25519 AAAA patchpilot",
	}, nil)
	if err != nil {
		t.Fatalf("BootstrapStandardWithRunner() error: %v", err)
	}
	batchIndex := runner.indexContaining("apt-get install -y sudo")
	if batchIndex == -1 {
		t.Fatalf("commands = %#v; want su bootstrap batch", runner.commands)
	}
	if !runner.options[batchIndex].PTY {
		t.Fatal("su bootstrap batch did not request PTY")
	}
	if !strings.Contains(runner.commands[batchIndex], "su -c") {
		t.Fatalf("batch command = %q, want su wrapper", runner.commands[batchIndex])
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "root-secret") || strings.Contains(strings.Join(runner.commands, "\n"), "user-secret") {
		t.Fatalf("secret leaked into commands: %#v", runner.commands)
	}
	if got := string(runner.stdin[batchIndex]); got != "root-secret" {
		t.Fatalf("batch stdin = %q, want raw root password for PTY writer", got)
	}
}

func TestBootstrapStandardWithRunnerUsesSuWhenSudoPresentButDenied(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"sudo -S":                    {result: remotessh.CommandResult{Stderr: "database is not in the sudoers file."}, err: errors.New("exit status 1")},
		"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: aptResolvedPathsOutput()}},
	}}
	_, err := BootstrapStandardWithRunner(context.Background(), runner, BootstrapOptions{
		Facts:        HostFacts{CurrentUID: "1000", PackageManager: pkgmgr.ManagerAPT, SudoPresent: true, SuPresent: true},
		Manager:      pkgmgr.ManagerAPT,
		Password:     []byte("user-secret"),
		RootPassword: []byte("root-secret"),
		PublicKey:    "ssh-ed25519 AAAA patchpilot",
	}, nil)
	if err != nil {
		t.Fatalf("BootstrapStandardWithRunner() error: %v", err)
	}
	if runner.indexContaining("sudo -S") != -1 {
		t.Fatalf("commands = %#v; sudo-denied su path must not try current-user sudo", runner.commands)
	}
	batchIndex := runner.indexContaining("prepare ssh directory")
	if batchIndex == -1 {
		t.Fatalf("commands = %#v; want su bootstrap batch", runner.commands)
	}
	if strings.Contains(runner.commands[batchIndex], "apt-get install -y sudo") {
		t.Fatalf("batch should not reinstall sudo when SudoPresent is true: %q", runner.commands[batchIndex])
	}
	if !runner.options[batchIndex].PTY {
		t.Fatal("su bootstrap batch did not request PTY")
	}
}

func TestBootstrapDirectRootInstallsSudoBeforeWritingSudoers(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: aptResolvedPathsOutput() + "id\t/usr/bin/id\ninstall\t/usr/bin/install\nchown\t/usr/bin/chown\nchmod\t/usr/bin/chmod\nuseradd\t/usr/sbin/useradd\nvisudo\t/usr/sbin/visudo\nrm\t/usr/bin/rm\nuserdel\t/usr/sbin/userdel\n"}},
	}}
	_, err := BootstrapStandardWithRunner(context.Background(), runner, BootstrapOptions{
		Facts:     HostFacts{CurrentUID: "0", PackageManager: pkgmgr.ManagerAPT, SudoPresent: false},
		Manager:   pkgmgr.ManagerAPT,
		PublicKey: "ssh-ed25519 AAAA patchpilot",
	}, nil)
	if err != nil {
		t.Fatalf("BootstrapStandardWithRunner() error: %v", err)
	}
	installSudoIndex := runner.indexContaining("apt-get install -y sudo")
	prepareIndex := runner.indexContaining("/usr/bin/install -d -m 0750 /etc/sudoers.d")
	writeIndex := runner.indexContaining("cat > /etc/sudoers.d/patchpilot")
	verifyIndex := runner.indexContaining("/usr/sbin/visudo -cf /etc/sudoers.d/patchpilot")
	for name, index := range map[string]int{"install sudo": installSudoIndex, "prepare sudoers dir": prepareIndex, "write sudoers": writeIndex, "visudo": verifyIndex} {
		if index == -1 {
			t.Fatalf("commands = %#v; missing %s", runner.commands, name)
		}
	}
	if installSudoIndex >= prepareIndex || prepareIndex >= writeIndex || writeIndex >= verifyIndex {
		t.Fatalf("unexpected order install=%d prepare=%d write=%d verify=%d commands=%#v", installSudoIndex, prepareIndex, writeIndex, verifyIndex, runner.commands)
	}
}

func TestBootstrapHardFailsWhenSudoersIncludeMissing(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: aptResolvedPathsOutput()}},
		"grep -Eq":                   {result: remotessh.CommandResult{Stderr: "/etc/sudoers does not include @includedir /etc/sudoers.d"}, err: errors.New("exit status 11")},
	}}
	_, err := BootstrapStandardWithRunner(context.Background(), runner, BootstrapOptions{
		Facts:     HostFacts{CurrentUID: "0", PackageManager: pkgmgr.ManagerAPT, SudoPresent: true},
		Manager:   pkgmgr.ManagerAPT,
		PublicKey: "ssh-ed25519 AAAA patchpilot",
	}, nil)
	if err == nil {
		t.Fatal("BootstrapStandardWithRunner() expected sudoers include failure")
	}
	if !strings.Contains(err.Error(), "@includedir /etc/sudoers.d") {
		t.Fatalf("error = %q, want includedir guidance", err.Error())
	}
	if runner.indexContaining("cat > /etc/sudoers.d/patchpilot") != -1 {
		t.Fatalf("commands = %#v; must not write sudoers when include precheck fails", runner.commands)
	}
}

func TestClassifyBootstrapFailureNamesCommonPreconditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		step     string
		command  string
		result   remotessh.CommandResult
		wantCode BootstrapErrorCode
		wantText string
	}{
		{name: "sudoers include", step: "Checking sudoers include", result: remotessh.CommandResult{Stderr: "add @includedir /etc/sudoers.d"}, wantCode: BootstrapErrorSudoersInclude, wantText: "@includedir /etc/sudoers.d"},
		{name: "package lock", step: "Installing sudo", result: remotessh.CommandResult{Stderr: "Could not get lock /var/lib/dpkg/lock-frontend"}, wantCode: BootstrapErrorPackageLock, wantText: "package manager is locked"},
		{name: "network repo", step: "Installing sudo", result: remotessh.CommandResult{Stderr: "Temporary failure resolving deb.debian.org"}, wantCode: BootstrapErrorNetwork, wantText: "repository"},
		{name: "sudo password", step: "Verifying NOPASSWD sudo", result: remotessh.CommandResult{Stderr: "sudo: a password is required"}, wantCode: BootstrapErrorNOPASSWD, wantText: "without a password"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ClassifyBootstrapFailure(test.step, test.command, test.result, errors.New("exit status 1"))
			var classified *BootstrapError
			if !errors.As(err, &classified) {
				t.Fatalf("classified error type = %T", err)
			}
			if classified.Code != test.wantCode {
				t.Fatalf("Code = %q, want %q", classified.Code, test.wantCode)
			}
			if !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("error = %q, want %q", err.Error(), test.wantText)
			}
		})
	}
}

func TestRollbackBootstrapDoesNotUninstallSudo(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	RollbackBootstrap(context.Background(), runner, HostFacts{CurrentUID: "0", PackageManager: pkgmgr.ManagerAPT}, nil, nil, pkgmgr.ManagerAPT, map[string]string{
		"apt-get": "/usr/bin/apt-get",
		"apt":     "/usr/bin/apt",
		"dpkg":    "/usr/bin/dpkg",
		"test":    "/usr/bin/test",
	}, nil)
	joined := strings.Join(runner.commands, "\n")
	for _, forbidden := range []string{"remove sudo", "purge sudo", "uninstall sudo"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("rollback must not uninstall sudo; commands=%#v", runner.commands)
		}
	}
}

func TestResolveCommandPathsStripsLeadingSuPasswordPrompt(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: "Password:\r\napt-get\t/usr/bin/apt-get\n"}},
	}}
	paths, err := resolveRequiredCommandPathsWithEscalation(context.Background(), runner, nil, escalationInput{Facts: HostFacts{CurrentUID: "1000", SuPresent: true}, RootPassword: []byte("root-secret")}, []string{"apt-get"})
	if err != nil {
		t.Fatalf("resolveRequiredCommandPathsWithEscalation() error: %v", err)
	}
	if paths["apt-get"] != "/usr/bin/apt-get" {
		t.Fatalf("paths[apt-get] = %q", paths["apt-get"])
	}
}

func TestStripLeadingSuPasswordPromptsLeavesNormalOutput(t *testing.T) {
	t.Parallel()

	output := "apt-get\t/usr/bin/apt-get\nPassword: appears later\n"
	if got := stripLeadingSuPasswordPrompts(output); got != output {
		t.Fatalf("stripLeadingSuPasswordPrompts() = %q, want unchanged %q", got, output)
	}
}

func TestSuPromptOnlyFailureMessage(t *testing.T) {
	t.Parallel()

	message := suFailureMessage(remotessh.CommandResult{Stderr: "Password:"}, errors.New("context deadline exceeded"))
	if message != "su did not accept the root password (prompt not answered or password rejected)" {
		t.Fatalf("message = %q", message)
	}
}

func aptResolvedPathsOutput() string {
	return "apt-get\t/usr/bin/apt-get\napt\t/usr/bin/apt\ndpkg\t/usr/bin/dpkg\ntest\t/usr/bin/test\nsystemctl\t/usr/bin/systemctl\nreboot\t/usr/sbin/reboot\nshutdown\t/usr/sbin/shutdown\nid\t/usr/bin/id\ninstall\t/usr/bin/install\nchown\t/usr/bin/chown\nchmod\t/usr/bin/chmod\nrm\t/usr/bin/rm\nuseradd\t/usr/sbin/useradd\nusermod\t/usr/sbin/usermod\ngroupadd\t/usr/sbin/groupadd\nuserdel\t/usr/sbin/userdel\nvisudo\t/usr/sbin/visudo\ndeluser\t/usr/sbin/deluser\n"
}

type fakeResponse struct {
	result remotessh.CommandResult
	err    error
}

type fakeRunner struct {
	commands  []string
	stdin     [][]byte
	options   []remotessh.RunOptions
	closed    bool
	responses map[string]fakeResponse
}

func (r *fakeRunner) Run(ctx context.Context, command string, stdin []byte) (remotessh.CommandResult, error) {
	return r.RunWithOptions(ctx, command, stdin, remotessh.RunOptions{})
}

func (r *fakeRunner) RunWithOptions(_ context.Context, command string, stdin []byte, options remotessh.RunOptions) (remotessh.CommandResult, error) {
	r.commands = append(r.commands, command)
	r.stdin = append(r.stdin, append([]byte{}, stdin...))
	r.options = append(r.options, options)
	for needle, response := range r.responses {
		if strings.Contains(command, needle) {
			return response.result, response.err
		}
	}
	return remotessh.CommandResult{}, nil
}

func (r *fakeRunner) Close() error {
	r.closed = true
	return nil
}

func (r *fakeRunner) indexContaining(needle string) int {
	for index, command := range r.commands {
		if strings.Contains(command, needle) {
			return index
		}
	}
	return -1
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
