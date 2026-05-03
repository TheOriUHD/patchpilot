package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/stream"
)

// RegisterWizard mounts Standard wizard endpoints.
func RegisterWizard(router chi.Router, service *core.HostService) {
	router.Route("/api/wizard", func(r chi.Router) {
		r.Post("/standard/detect", func(w http.ResponseWriter, req *http.Request) {
			var input standardDetectRequest
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			password := []byte(input.Password)
			defer core.ZeroBytes(password)
			result, err := service.DetectStandard(req.Context(), core.StandardDetectInput{Hostname: input.Hostname, SSHPort: input.SSHPort, SSHUser: input.SSHUser, Password: password})
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
		})

		r.Post("/standard/root-assist", func(w http.ResponseWriter, req *http.Request) {
			var input rootAssistRequest
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			password := []byte(input.RootPassword)
			defer core.ZeroBytes(password)
			if err := service.RootAssist(req.Context(), core.RootAssistInput{Hostname: input.Hostname, SSHPort: input.SSHPort, SSHUser: input.SSHUser, RootPassword: password, Facts: input.Facts, Diagnosis: input.Diagnosis}); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		r.Post("/standard/bootstrap", func(w http.ResponseWriter, req *http.Request) {
			var input bootstrapStartRequest
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			password := []byte(input.Password)
			defer core.ZeroBytes(password)
			session, err := service.StartBootstrap(req.Context(), core.BootstrapStartInput{Hostname: input.Hostname, SSHPort: input.SSHPort, SSHUser: input.SSHUser, Password: password, Facts: input.Facts})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusAccepted, session)
		})

		r.Post("/{id}/commit", func(w http.ResponseWriter, req *http.Request) {
			var input core.CommitHostInput
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			input.SessionID = chi.URLParam(req, "id")
			host, err := service.CommitBootstrap(req.Context(), input)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusCreated, host)
		})

		r.Post("/{id}/cancel", func(w http.ResponseWriter, req *http.Request) {
			if err := service.CancelBootstrap(req.Context(), chi.URLParam(req, "id")); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		r.Get("/{id}/stream", func(w http.ResponseWriter, req *http.Request) {
			streamID := chi.URLParam(req, "id")
			events, err := service.Streams.Subscribe(req.Context(), streamID)
			if err != nil {
				writeError(w, http.StatusNotFound, err)
				return
			}
			serveSSE(req.Context(), w, events)
		})
	})
}

type standardDetectRequest struct {
	Hostname string `json:"hostname"`
	SSHPort  int    `json:"ssh_port"`
	SSHUser  string `json:"ssh_user"`
	Password string `json:"password"`
}

type rootAssistRequest struct {
	Hostname     string                    `json:"hostname"`
	SSHPort      int                       `json:"ssh_port"`
	SSHUser      string                    `json:"ssh_user"`
	RootPassword string                    `json:"root_password"`
	Facts        core.HostFacts            `json:"facts"`
	Diagnosis    *core.PermissionDiagnosis `json:"diagnosis,omitempty"`
}

type bootstrapStartRequest struct {
	Hostname string         `json:"hostname"`
	SSHPort  int            `json:"ssh_port"`
	SSHUser  string         `json:"ssh_user"`
	Password string         `json:"password"`
	Facts    core.HostFacts `json:"facts"`
}

func serveSSE(ctx context.Context, w http.ResponseWriter, events <-chan stream.Event) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "id: %d\n", event.ID)
			_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(event.Data))
			flusher.Flush()
		}
	}
}
