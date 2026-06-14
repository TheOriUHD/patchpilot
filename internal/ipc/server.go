package ipc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Server hosts the daemon HTTP API on a Unix socket.
type Server struct {
	socketPath string
	handler    http.Handler
	logger     *slog.Logger
	ready      func() error
}

// NewServer creates a Unix-socket HTTP API server.
func NewServer(socketPath string, handler http.Handler, logger *slog.Logger) *Server {
	return &Server{socketPath: socketPath, handler: handler, logger: logger}
}

// SetReadyFunc installs a callback invoked after the IPC listener is active.
func (s *Server) SetReadyFunc(ready func() error) {
	s.ready = ready
}

// Serve starts the server and blocks until the context is canceled or serving fails.
func (s *Server) Serve(ctx context.Context) error {
	if err := prepareSocket(ctx, s.socketPath); err != nil {
		return err
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on unix socket %s: %w", s.socketPath, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(s.socketPath)
	}()
	if err := os.Chmod(s.socketPath, 0o660); err != nil {
		return fmt.Errorf("set socket permissions on %s: %w", s.socketPath, err)
	}

	server := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	s.logger.Info("daemon ipc server listening", "socket", s.socketPath)
	if s.ready != nil {
		if err := s.ready(); err != nil {
			_ = server.Close()
			return fmt.Errorf("notify daemon readiness: %w", err)
		}
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown ipc server: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve ipc server: %w", err)
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve ipc server: %w", err)
		}
		return nil
	}
}

func prepareSocket(ctx context.Context, socketPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	if _, err := os.Stat(socketPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect socket path: %w", err)
	}

	dialer := net.Dialer{Timeout: 200 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("daemon socket %s is already active", socketPath)
	}

	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale socket %s: %w", socketPath, err)
	}
	return nil
}
