package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const bootstrapDebugPreferredPath = "/var/log/patchpilot/bootstrap-debug.log"
const bootstrapDebugFallbackPath = "bootstrap-debug.log"

var bootstrapDebugMu sync.Mutex
var bootstrapDebugPathLogged bool

// BootstrapDebugf writes one bootstrap diagnostic line to the shared diagnostic log.
func BootstrapDebugf(format string, args ...any) {
	bootstrapDebugMu.Lock()
	defer bootstrapDebugMu.Unlock()

	file, path, err := openBootstrapDebugLog()
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	if !bootstrapDebugPathLogged {
		_, _ = fmt.Fprintf(file, "%s bootstrap debug log path: %s\n", time.Now().Format(time.RFC3339Nano), path)
		bootstrapDebugPathLogged = true
	}
	_, _ = fmt.Fprintf(file, "%s ", time.Now().Format(time.RFC3339Nano))
	_, _ = fmt.Fprintf(file, format, args...)
	_, _ = fmt.Fprintln(file)
}

func openBootstrapDebugLog() (*os.File, string, error) {
	file, err := openLogAt(bootstrapDebugPreferredPath)
	if err == nil {
		return file, bootstrapDebugPreferredPath, nil
	}
	file, fallbackErr := openLogAt(bootstrapDebugFallbackPath)
	if fallbackErr == nil {
		return file, bootstrapDebugFallbackPath, nil
	}
	return nil, "", err
}

func openLogAt(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
