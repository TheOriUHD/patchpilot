package handlers

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const bootstrapStreamDebugPreferredPath = "/var/log/patchpilot/bootstrap-debug.log"
const bootstrapStreamDebugFallbackPath = "bootstrap-debug.log"

var bootstrapStreamDebugMu sync.Mutex
var bootstrapStreamDebugPathLogged bool

func bootstrapStreamDebugf(format string, args ...any) {
	file, path, err := openBootstrapStreamDebugLog()
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	bootstrapStreamDebugMu.Lock()
	if !bootstrapStreamDebugPathLogged {
		_, _ = fmt.Fprintf(file, "%s bootstrap stream debug log path: %s\n", time.Now().Format(time.RFC3339Nano), path)
		bootstrapStreamDebugPathLogged = true
	}
	bootstrapStreamDebugMu.Unlock()
	_, _ = fmt.Fprintf(file, "%s ", time.Now().Format(time.RFC3339Nano))
	_, _ = fmt.Fprintf(file, format, args...)
	_, _ = fmt.Fprintln(file)
}

func openBootstrapStreamDebugLog() (*os.File, string, error) {
	file, err := os.OpenFile(bootstrapStreamDebugPreferredPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		return file, bootstrapStreamDebugPreferredPath, nil
	}
	file, fallbackErr := os.OpenFile(bootstrapStreamDebugFallbackPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if fallbackErr == nil {
		return file, bootstrapStreamDebugFallbackPath, nil
	}
	return nil, "", err
}
