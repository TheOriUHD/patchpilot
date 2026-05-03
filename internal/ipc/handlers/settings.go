package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/core"
)

// RegisterSettings mounts settings endpoints.
func RegisterSettings(router chi.Router, service *core.SettingsService) {
	router.Route("/api/settings", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			snapshot, err := service.Snapshot(req.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, snapshot)
		})

		r.Put("/concurrency", func(w http.ResponseWriter, req *http.Request) {
			var input core.SettingsUpdate
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if err := service.SetConcurrency(req.Context(), input.ConcurrencyEnabled); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			snapshot, err := service.Snapshot(req.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, snapshot)
		})

		r.Post("/ssh/rotate", func(w http.ResponseWriter, req *http.Request) {
			if err := service.RotateKeypair(req.Context()); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		r.Post("/inventory/export", func(w http.ResponseWriter, req *http.Request) {
			var input core.InventoryPathRequest
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if err := service.ExportInventory(req.Context(), input.Path); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		r.Post("/inventory/import", func(w http.ResponseWriter, req *http.Request) {
			var input core.InventoryPathRequest
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			result, err := service.ImportInventory(req.Context(), input)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
		})
	})
}
