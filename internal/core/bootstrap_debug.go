package core

import (
	"fmt"
	"os"
	"time"
)

func bootstrapDebugf(format string, args ...any) {
	file, err := os.OpenFile("/tmp/patchpilot-bootstrap.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_, _ = fmt.Fprintf(file, "%s ", time.Now().Format(time.RFC3339Nano))
	_, _ = fmt.Fprintf(file, format, args...)
	_, _ = fmt.Fprintln(file)
}
