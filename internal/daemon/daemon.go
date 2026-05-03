package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/config"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/ipc"
	"github.com/TheOriUHD/patchpilot/internal/logging"
	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
	"github.com/TheOriUHD/patchpilot/internal/store"
	"github.com/TheOriUHD/patchpilot/internal/stream"
	"github.com/TheOriUHD/patchpilot/migrations"
)

// Run starts the persistent patchpilot daemon.
func Run(ctx context.Context) error {
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, closeLogger, err := logging.NewDaemonLogger(ctx, cfg.Paths.LogPath, os.Stderr)
	if err != nil {
		return fmt.Errorf("configure daemon logger: %w", err)
	}
	defer func() {
		if err := closeLogger(); err != nil {
			slog.Default().Error("close daemon logger", "error", err)
		}
	}()

	logger.Info("starting patchpilot daemon", "database", cfg.Paths.DatabasePath, "socket", cfg.Paths.SocketPath)

	db, err := store.Open(ctx, cfg.Paths.DatabasePath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("close store", "error", err)
		}
	}()

	if err := db.Migrate(ctx, migrations.Files); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	recovered, err := db.RecoverOrphanedRuns(ctx, "daemon stopped while this run was active; mark failed on startup recovery")
	if err != nil {
		return fmt.Errorf("recover orphaned runs: %w", err)
	}
	if recovered > 0 {
		logger.Warn("recovered orphaned running runs", "count", recovered)
	}

	hostRepo := store.NewHostRepository(db)
	runRepo := store.NewRunRepository(db)
	auditRepo := store.NewAuditRepository(db)
	keyStore := remotessh.NewKeyStore(cfg.Paths.DataDir)
	streamHub := stream.NewHub()
	hostService := core.NewHostService(hostRepo, runRepo, auditRepo, keyStore, streamHub, logger, filepath.Join(filepath.Dir(cfg.Paths.LogPath), "runs"))

	router := ipc.NewRouter(time.Now().UTC(), hostService)
	server := ipc.NewServer(cfg.Paths.SocketPath, router, logger)
	if err := server.Serve(ctx); err != nil {
		return fmt.Errorf("serve daemon api: %w", err)
	}
	logger.Info("patchpilot daemon stopped")
	return nil
}
