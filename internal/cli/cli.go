package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/config"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/daemon"
	"github.com/TheOriUHD/patchpilot/internal/ipc"
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
	case "add":
		return fmt.Errorf("host add is available in the TUI wizard; run patchpilot tui and press a")
	case "list":
		client, err := daemonClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hosts, err := client.ListHosts(ctx)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "%-5s %-24s %-12s %-14s %s\n", "ID", "Name", "Type", "Package", "Host")
		for _, host := range hosts {
			_, _ = fmt.Fprintf(stdout, "%-5d %-24s %-12s %-14s %s:%d\n", host.ID, host.Name, host.Type, host.PackageManager, host.Hostname, host.SSHPort)
		}
		return nil
	case "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: patchpilot host run <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("parse host id: %w", err)
		}
		client, err := daemonClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		runID, err := client.RunHostNow(ctx, id)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Started manual run %d for host %d\n", runID, id)
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: patchpilot host remove <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("parse host id: %w", err)
		}
		client, err := daemonClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.DeleteHost(ctx, id); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Removed host %d\n", id)
		return nil
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
	case "add":
		return runScheduleAdd(args[1:], stdout)
	case "list":
		client, err := daemonClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		schedules, err := client.ListSchedules(ctx)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "%-5s %-24s %-10s %-8s %-7s %s\n", "ID", "Name", "Freq", "Enabled", "Hosts", "Next")
		for _, schedule := range schedules {
			next := "-"
			if schedule.NextRunAt != nil {
				next = schedule.NextRunAt.Format(time.RFC3339)
			}
			_, _ = fmt.Fprintf(stdout, "%-5d %-24s %-10s %-8v %-7d %s\n", schedule.ID, schedule.Name, schedule.Frequency, schedule.Enabled, schedule.HostCount, next)
		}
		return nil
	case "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: patchpilot schedule run <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("parse schedule id: %w", err)
		}
		client, err := daemonClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		detail, err := client.RunScheduleNow(ctx, id)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Started schedule run %d with status %s\n", detail.Run.ID, detail.Run.Status)
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: patchpilot schedule remove <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("parse schedule id: %w", err)
		}
		client, err := daemonClient()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.DeleteSchedule(ctx, id); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Removed schedule %d\n", id)
		return nil
	default:
		return fmt.Errorf("unknown schedule command %q", args[0])
	}
}

func runScheduleAdd(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("schedule add", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		parseArgs = args[1:]
	}
	frequency := flags.String("frequency", "daily", "hourly, daily, weekly, or monthly")
	hour := flags.Int("hour", 3, "hour of day")
	minute := flags.Int("minute", 0, "minute")
	dayOfWeek := flags.Int("day-of-week", 0, "Sunday=0 through Saturday=6")
	dayOfMonth := flags.Int("day-of-month", 1, "day of month, 1-28")
	hosts := flags.String("hosts", "", "comma-separated host ids")
	disabled := flags.Bool("disabled", false, "create disabled")
	if err := flags.Parse(parseArgs); err != nil {
		return err
	}
	if name == "" && flags.NArg() > 0 {
		name = strings.Join(flags.Args(), " ")
	}
	if name == "" {
		return fmt.Errorf("usage: patchpilot schedule add <name> --frequency daily --hour 3 --minute 0 --hosts 1,2")
	}
	hostIDs, err := parseIDList(*hosts)
	if err != nil {
		return err
	}
	client, err := daemonClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schedule, err := client.CreateSchedule(ctx, core.ScheduleInput{Name: name, Frequency: core.Frequency(*frequency), Hour: *hour, Minute: *minute, DayOfWeek: *dayOfWeek, DayOfMonth: *dayOfMonth, Enabled: !*disabled, HostIDs: hostIDs})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Created schedule %d (%s)\n", schedule.ID, schedule.Name)
	return nil
}

func parseIDList(value string) ([]int64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse host id %q: %w", part, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: patchpilot [tui|daemon|host|schedule]")
}

func daemonClient() (*ipc.Client, error) {
	cfg, err := config.Load(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return ipc.NewClient(cfg.Paths.SocketPath), nil
}
