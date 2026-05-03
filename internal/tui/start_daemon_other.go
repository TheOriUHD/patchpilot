//go:build !linux

package tui

import (
	"context"
	"fmt"
)

func startDaemon(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("systemd is not available on this platform; start the daemon manually with patchpilot daemon")
}
