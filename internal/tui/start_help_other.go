//go:build !linux

package tui

func daemonStartAction() string {
	return "start a local development daemon"
}

func daemonManualCommand() string {
	return "patchpilot daemon"
}
