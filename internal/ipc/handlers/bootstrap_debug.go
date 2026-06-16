package handlers

import "github.com/TheOriUHD/patchpilot/internal/diagnostics"

func bootstrapStreamDebugf(format string, args ...any) {
	diagnostics.BootstrapDebugf(format, args...)
}
