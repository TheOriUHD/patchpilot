package ipc

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/ipc/handlers"
)

// NewRouter builds the daemon HTTP API routes.
func NewRouter(startedAt time.Time) http.Handler {
	router := chi.NewRouter()
	handlers.RegisterHealth(router, startedAt)
	return router
}
