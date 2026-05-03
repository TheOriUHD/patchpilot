package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/core"
)

// RegisterHosts mounts host inventory endpoints.
func RegisterHosts(router chi.Router, service *core.HostService, orchestrator *core.Orchestrator) {
	router.Route("/api/hosts", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			hosts, err := service.Hosts.List(req.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, hosts)
		})

		r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			host, err := service.Hosts.Get(req.Context(), id)
			if err != nil {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeJSON(w, http.StatusOK, host)
		})

		r.Put("/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			var input core.HostUpdate
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			host, err := service.Hosts.Update(req.Context(), id, input)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			serviceAudit(service, req.Context(), "tui", "host.update", "host", &host.ID)
			writeJSON(w, http.StatusOK, host)
		})

		r.Post("/{id}/toggle", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			host, err := service.Hosts.Get(req.Context(), id)
			if err != nil {
				writeError(w, http.StatusNotFound, err)
				return
			}
			host, err = service.Hosts.SetEnabled(req.Context(), id, !host.Enabled)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			serviceAudit(service, req.Context(), "tui", "host.toggle", "host", &host.ID)
			writeJSON(w, http.StatusOK, host)
		})

		r.Delete("/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if err := service.Hosts.Delete(req.Context(), id); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			serviceAudit(service, req.Context(), "tui", "host.remove", "host", &id)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		r.Post("/{id}/dry-run", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			result, err := service.DryRunHost(req.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
		})

		r.Post("/{id}/run", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			detail, err := orchestrator.RunHostNow(req.Context(), id, core.RunTriggerManual, "tui")
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusAccepted, detail)
		})
	})
}

func serviceAudit(service *core.HostService, ctx context.Context, actor string, action string, targetType string, targetID *int64) {
	if service.Audit == nil {
		return
	}
	_ = service.Audit.Create(ctx, core.AuditEvent{Actor: actor, Action: action, TargetType: targetType, TargetID: targetID, Details: "{}"})
}
