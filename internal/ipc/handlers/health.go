package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/version"
)

// RegisterHealth mounts the health endpoint on the router.
func RegisterHealth(router chi.Router, startedAt time.Time) {
	router.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		info := version.Info()
		response := healthResponse{
			Status:    "ok",
			Version:   info.Version,
			Commit:    info.Commit,
			BuildDate: info.BuildDate,
			StartedAt: startedAt,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "encode health response", http.StatusInternalServerError)
			return
		}
	})
}

type healthResponse struct {
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildDate string    `json:"build_date"`
	StartedAt time.Time `json:"started_at"`
}
