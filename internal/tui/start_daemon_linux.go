//go:build linux

package tui

import (
	"context"
	"fmt"
	"os/exec"
)

func startDaemon(ctx context.Context) error {
	command := exec.CommandContext(ctx, "sudo", "-n", "systemctl", "start", "patchpilot")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sudo systemctl start patchpilot failed: %w: %s", err, string(output))
	}
	return nil
}
