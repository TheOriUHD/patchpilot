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
		"resolve_patchpilot_command": {result: remotessh.CommandResult{Stdout: "apt-get\t/usr/bin/apt-get\napt\t/usr/bin/apt\ndpkg\t/usr/bin/dpkg\ntest\t/usr/bin/test\n"}},
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
		"apt-get": "/opt/bin/apt-get",
		"apt":     "/opt/bin/apt",
		"dpkg":    "/opt/bin/dpkg",
		"test":    "/bin/test",
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
}

type fakeResponse struct {
	result remotessh.CommandResult
	err    error
}

type fakeRunner struct {
	commands  []string
	closed    bool
	responses map[string]fakeResponse
}

func (r *fakeRunner) Run(_ context.Context, command string, _ []byte) (remotessh.CommandResult, error) {
	r.commands = append(r.commands, command)
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
