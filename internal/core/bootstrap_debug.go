package core

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const bootstrapDebugPreferredPath = "/var/log/patchpilot/bootstrap-debug.log"
const bootstrapDebugFallbackPath = "bootstrap-debug.log"

var bootstrapDebugMu sync.Mutex
var bootstrapDebugPathLogged bool

func bootstrapDebugf(format string, args ...any) {
	file, path, err := openBootstrapDebugLog()
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	bootstrapDebugMu.Lock()
	if !bootstrapDebugPathLogged {
		_, _ = fmt.Fprintf(file, "%s bootstrap debug log path: %s\n", time.Now().Format(time.RFC3339Nano), path)
		bootstrapDebugPathLogged = true
	}
	bootstrapDebugMu.Unlock()
	_, _ = fmt.Fprintf(file, "%s ", time.Now().Format(time.RFC3339Nano))
	_, _ = fmt.Fprintf(file, format, args...)
	_, _ = fmt.Fprintln(file)
}

func openBootstrapDebugLog() (*os.File, string, error) {
	file, err := os.OpenFile(bootstrapDebugPreferredPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		return file, bootstrapDebugPreferredPath, nil
	}
	file, fallbackErr := os.OpenFile(bootstrapDebugFallbackPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if fallbackErr == nil {
		return file, bootstrapDebugFallbackPath, nil
	}
	return nil, "", err
}
