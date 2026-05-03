package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

// ErrUnsupported is returned outside Linux production environments.
var ErrUnsupported = errors.New("self-update not supported on macOS development environment")

// Scheduler creates one-shot systemd transient self-update units.
type Scheduler struct {
	LogDir     string
	RunCommand func(ctx context.Context, name string, args ...string) error
}

// NewScheduler creates a self-update scheduler that writes logs below logDir.
func NewScheduler(logDir string) *Scheduler {
	return &Scheduler{LogDir: logDir}
}

// LogPath returns the self-update log path for a run.
func (s *Scheduler) LogPath(runID int64) string {
	return filepath.Join(s.LogDir, fmt.Sprintf("self-update-%d.log", runID))
}

// Schedule creates a one-shot systemd-run unit on Linux.
func (s *Scheduler) Schedule(ctx context.Context, runID int64) (string, string, error) {
	logPath := s.LogPath(runID)
	if runtime.GOOS != "linux" {
		return "", logPath, ErrUnsupported
	}
	if err := os.MkdirAll(s.LogDir, 0o755); err != nil {
		return "", logPath, fmt.Errorf("create self-update log directory: %w", err)
	}
	name, args := BuildSystemdRun(runID, logPath)
	runner := s.RunCommand
	if runner == nil {
		runner = func(ctx context.Context, name string, args ...string) error {
			return exec.CommandContext(ctx, name, args...).Run()
		}
	}
	if err := runner(ctx, name, args...); err != nil {
		return "", logPath, fmt.Errorf("schedule self-update: %w", err)
	}
	return UnitName(runID), logPath, nil
}

// UnitName returns the systemd unit name for a self-update run.
func UnitName(runID int64) string {
	return "patchpilot-self-update-" + strconv.FormatInt(runID, 10) + ".service"
}

// BuildSystemdRun returns the systemd-run executable and arguments.
func BuildSystemdRun(runID int64, logPath string) (string, []string) {
	command := "apt-get update && DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=\"--force-confdef\" -o Dpkg::Options::=\"--force-confold\" dist-upgrade && apt-get -y autoremove"
	command += " > " + shellQuote(logPath) + " 2>&1"
	return "systemd-run", []string{"--unit", UnitName(runID), "--on-active=30s", "/bin/sh", "-c", command}
}

func shellQuote(value string) string {
	quoted := "'"
	for _, char := range value {
		if char == '\'' {
			quoted += "'\\''"
			continue
		}
		quoted += string(char)
	}
	return quoted + "'"
}
