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
		"apt-get":    {result: remotessh.CommandResult{Stdout: "/usr/bin/apt-get\n"}},
		"'apt'":      {result: remotessh.CommandResult{Stdout: "/usr/bin/apt\n"}},
		"dpkg":       {result: remotessh.CommandResult{Stdout: "/usr/bin/dpkg\n"}},
		"test":       {result: remotessh.CommandResult{Stdout: "/bin/test\n"}},
		"visudo -cf": {result: remotessh.CommandResult{Stderr: "parse error"}, err: errors.New("exit status 1")},
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
