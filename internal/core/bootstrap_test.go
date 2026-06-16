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
	if got := string(escalated.Stdin); got != "root-secret\n" {
		t.Fatalf("stdin = %q, want root password plus newline", got)
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
	})
	if err != nil {
		t.Fatalf("GenerateBootstrapCommands() error: %v", err)
	}
	batch, err := suBootstrapBatch(pkgmgr.ManagerAPT, commands, true)
	if err != nil {
		t.Fatalf("suBootstrapBatch() error: %v", err)
	}
	for _, want := range []string{
		"apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y sudo",
		"id patchpilot >/dev/null 2>&1 || useradd",
		"install -d -m 0700 -o patchpilot -g patchpilot /home/patchpilot/.ssh",
		"> /home/patchpilot/.ssh/authorized_keys",
		"cat > /etc/sudoers.d/patchpilot",
		"visudo -cf /etc/sudoers.d/patchpilot",
	} {
		if !strings.Contains(batch, want) {
			t.Fatalf("batch missing %q:\n%s", want, batch)
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
	if got := string(runner.stdin[batchIndex]); got != "root-secret\n" {
		t.Fatalf("batch stdin = %q, want root password plus newline", got)
	}
}

func aptResolvedPathsOutput() string {
	return "apt-get\t/usr/bin/apt-get\napt\t/usr/bin/apt\ndpkg\t/usr/bin/dpkg\ntest\t/usr/bin/test\nsystemctl\t/usr/bin/systemctl\nreboot\t/usr/sbin/reboot\nshutdown\t/usr/sbin/shutdown\n"
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
