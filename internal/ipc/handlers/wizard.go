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

		r.Post("/api/verify", func(w http.ResponseWriter, req *http.Request) {
			var input apiHostRequest
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			token := []byte(input.APIToken)
			defer core.ZeroBytes(token)
			result, err := service.VerifyAPIHost(req.Context(), core.APIHostVerifyInput{Type: input.Type, Endpoint: input.Endpoint, APIToken: token})
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
		})

		r.Post("/api/check", func(w http.ResponseWriter, req *http.Request) {
			var input apiHostRequest
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			token := []byte(input.APIToken)
			defer core.ZeroBytes(token)
			result, err := service.CheckAPIHost(req.Context(), core.APIHostVerifyInput{Type: input.Type, Endpoint: input.Endpoint, APIToken: token})
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
		})

		r.Post("/api/commit", func(w http.ResponseWriter, req *http.Request) {
			var input apiHostCommitRequest
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			token := []byte(input.APIToken)
			defer core.ZeroBytes(token)
			host, err := service.CommitAPIHost(req.Context(), core.APIHostCommitInput{Type: input.Type, Endpoint: input.Endpoint, APIToken: token, Name: input.Name, Notes: input.Notes, Enabled: input.Enabled, AutoReboot: input.AutoReboot, AutoRebootDelay: input.AutoRebootDelay})
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusCreated, host)
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

type apiHostRequest struct {
	Type     core.HostType `json:"host_type"`
	Endpoint string        `json:"endpoint"`
	APIToken string        `json:"api_token"`
}

type apiHostCommitRequest struct {
	Type            core.HostType `json:"host_type"`
	Endpoint        string        `json:"endpoint"`
	APIToken        string        `json:"api_token"`
	Name            string        `json:"name"`
	Notes           string        `json:"notes"`
	Enabled         bool          `json:"enabled"`
	AutoReboot      bool          `json:"auto_reboot"`
	AutoRebootDelay int           `json:"auto_reboot_countdown_seconds"`
}

func serveSSE(ctx context.Context, w http.ResponseWriter, events <-chan stream.Event) {
	bootstrapStreamDebugf("serveSSE: start")
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
			bootstrapStreamDebugf("serveSSE: context done: %v", ctx.Err())
			return
		case event, ok := <-events:
			if !ok {
				bootstrapStreamDebugf("serveSSE: events channel closed")
				return
			}
			bootstrapStreamDebugf("serveSSE: forwarding event type=%q id=%d", event.Type, event.ID)
			_, _ = fmt.Fprintf(w, "id: %d\n", event.ID)
			_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(event.Data))
			flusher.Flush()
			if event.Type == "bootstrap.complete" {
				bootstrapStreamDebugf("serveSSE: forwarded final bootstrap.complete id=%d", event.ID)
			}
		}
	}
}
