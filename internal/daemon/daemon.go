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
	"github.com/TheOriUHD/patchpilot/internal/scheduler"
	"github.com/TheOriUHD/patchpilot/internal/secret"
	"github.com/TheOriUHD/patchpilot/internal/selfupdate"
	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
	"github.com/TheOriUHD/patchpilot/internal/store"
	"github.com/TheOriUHD/patchpilot/internal/stream"
	"github.com/TheOriUHD/patchpilot/internal/systemd"
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
	runLogRoot := filepath.Join(filepath.Dir(cfg.Paths.LogPath), "runs")
	runRepo := store.NewRunRepository(db, runLogRoot)
	scheduleRepo := store.NewScheduleRepository(db)
	settingsRepo := store.NewSettingsRepository(db)
	inventoryRepo := store.NewInventoryRepository(db)
	auditRepo := store.NewAuditRepository(db)
	keyStore := remotessh.NewKeyStore(cfg.Paths.DataDir)
	secretManager := secret.NewManager(cfg.Paths.SecretKeyPath)
	if _, err := secretManager.Ensure(ctx); err != nil {
		return fmt.Errorf("ensure secret key: %w", err)
	}
	streamHub := stream.NewHub()
	logTailer := stream.NewLogTailer(350 * time.Millisecond)
	hostService := core.NewHostService(hostRepo, auditRepo, keyStore, streamHub, logger, secretManager)
	orchestrator := core.NewOrchestrator(hostRepo, runRepo, scheduleRepo, auditRepo, keyStore, streamHub, logger, secretManager)
	orchestrator.SetSettingsStore(settingsRepo)
	orchestrator.SetSelfUpdater(selfupdate.NewScheduler(filepath.Dir(cfg.Paths.LogPath)))
	orchestrator.Start(ctx)
	scheduleEngine := scheduler.NewEngine(scheduleRepo, orchestrator, logger)
	if err := scheduleEngine.Start(ctx); err != nil {
		return fmt.Errorf("start scheduler: %w", err)
	}

	settingsService := &core.SettingsService{Hosts: hostRepo, Settings: settingsRepo, Inventory: inventoryRepo, Audit: auditRepo, Keys: keyStore, Paths: core.SettingsPaths{DataDir: cfg.Paths.DataDir, DatabasePath: cfg.Paths.DatabasePath, SocketPath: cfg.Paths.SocketPath, LogPath: cfg.Paths.LogPath, SecretKeyPath: cfg.Paths.SecretKeyPath}, LogPath: cfg.Paths.LogPath, Logger: logger}
	router := ipc.NewRouter(time.Now().UTC(), hostService, orchestrator, scheduleEngine, logTailer, settingsService)
	server := ipc.NewServer(cfg.Paths.SocketPath, router, logger)
	server.SetReadyFunc(systemd.NotifyReady)
	if err := server.Serve(ctx); err != nil {
		return fmt.Errorf("serve daemon api: %w", err)
	}
	logger.Info("patchpilot daemon stopped")
	return nil
}
