package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestClientSocketPathUsesEnvWithoutConfig(t *testing.T) {
	want := filepath.Join(t.TempDir(), "daemon.sock")
	t.Setenv(ClientSocketEnv, want)
	t.Setenv("PATCHPILOT_CONFIG", filepath.Join(t.TempDir(), "not-readable.yaml"))

	got, err := ClientSocketPath()
	if err != nil {
		t.Fatalf("ClientSocketPath() error: %v", err)
	}
	if got != want {
		t.Fatalf("ClientSocketPath() = %q, want %q", got, want)
	}
}

func TestClientSocketPathDefaults(t *testing.T) {
	t.Setenv(ClientSocketEnv, "")

	got, err := ClientSocketPath()
	if err != nil {
		t.Fatalf("ClientSocketPath() error: %v", err)
	}
	if runtime.GOOS == "linux" {
		if got != linuxSocketPath {
			t.Fatalf("ClientSocketPath() = %q, want %q", got, linuxSocketPath)
		}
		return
	}
	wantSuffix := filepath.Join(".patchpilot", "patchpilot.sock")
	if filepath.Base(filepath.Dir(got)) != ".patchpilot" || filepath.Base(got) != filepath.Base(wantSuffix) {
		t.Fatalf("ClientSocketPath() = %q, want path ending in %q", got, wantSuffix)
	}
}
