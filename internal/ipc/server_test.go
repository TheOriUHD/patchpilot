package ipc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerSetsSocketPermissions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tmpDir, err := os.MkdirTemp("/tmp", "patchpilot-ipc-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	socketPath := filepath.Join(tmpDir, "patchpilot.sock")
	server := NewServer(socketPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), slog.New(slog.NewTextHandler(io.Discard, nil)))

	ready := make(chan struct{})
	server.SetReadyFunc(func() error {
		close(ready)
		return nil
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ctx)
	}()

	select {
	case <-ready:
	case err := <-errCh:
		t.Fatalf("Serve() returned before ready: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not become ready")
	}

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o660 {
		t.Fatalf("socket mode = %04o, want 0660", got)
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	_ = conn.Close()

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() error after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down")
	}
}
