//go:build linux

package tui

func daemonStartAction() string {
	return "sudo systemctl start patchpilot"
}

func daemonManualCommand() string {
	return "sudo systemctl start patchpilot"
}
