package core

import "github.com/TheOriUHD/patchpilot/internal/diagnostics"

func bootstrapDebugf(format string, args ...any) {
	diagnostics.BootstrapDebugf(format, args...)
}
