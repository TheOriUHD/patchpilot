package ipc

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/ipc/handlers"
	"github.com/TheOriUHD/patchpilot/internal/scheduler"
	"github.com/TheOriUHD/patchpilot/internal/stream"
)

// NewRouter builds the daemon HTTP API routes.
func NewRouter(startedAt time.Time, hosts *core.HostService, orchestrator *core.Orchestrator, scheduleEngine *scheduler.Engine, logTailer *stream.LogTailer, settings *core.SettingsService) http.Handler {
	router := chi.NewRouter()
	handlers.RegisterHealth(router, startedAt)
	handlers.RegisterHosts(router, hosts, orchestrator)
	handlers.RegisterWizard(router, hosts)
	handlers.RegisterSchedules(router, orchestrator, scheduleEngine)
	handlers.RegisterRuns(router, orchestrator, logTailer)
	if settings != nil {
		handlers.RegisterSettings(router, settings)
	}
	return router
}
