package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/TheOriUHD/patchpilot/internal/daemon"
	"github.com/TheOriUHD/patchpilot/internal/tui"
)

// Run dispatches the patchpilot command line.
func Run(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return runTUI(stderr)
	}

	switch args[0] {
	case "tui":
		return runTUI(stderr)
	case "daemon":
		return runDaemon()
	case "host":
		return runHost(args[1:], stdout)
	case "schedule":
		return runSchedule(args[1:], stdout)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runTUI(stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := tui.Run(ctx); err != nil {
		_, _ = fmt.Fprintln(stderr, "The TUI exited before startup completed.")
		return err
	}
	return nil
}

func runDaemon() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return daemon.Run(ctx)
}

func runHost(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stdout, "Usage: patchpilot host <add|list|run|remove>")
		return nil
	}

	switch strings.ToLower(args[0]) {
	case "add", "list", "run", "remove":
		return fmt.Errorf("host %s is not available until host management is enabled", args[0])
	default:
		return fmt.Errorf("unknown host command %q", args[0])
	}
}

func runSchedule(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stdout, "Usage: patchpilot schedule <add|list|run|remove>")
		return nil
	}

	switch strings.ToLower(args[0]) {
	case "add", "list", "run", "remove":
		return fmt.Errorf("schedule %s is not available until schedule management is enabled", args[0])
	default:
		return fmt.Errorf("unknown schedule command %q", args[0])
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: patchpilot [tui|daemon|host|schedule]")
}
