package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/stream"
)

// RegisterRuns mounts run list, detail, cancellation, and stream endpoints.
func RegisterRuns(router chi.Router, orchestrator *core.Orchestrator, tailer *stream.LogTailer) {
	router.Route("/api/runs", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			runs, err := orchestrator.ListRunsPage(req.Context(), runListQuery(req))
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, runs)
		})

		r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			detail, err := orchestrator.GetRun(req.Context(), id)
			if err != nil {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeJSON(w, http.StatusOK, detail)
		})

		r.Get("/{id}/logs", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			logs, err := orchestrator.RunLogs(req.Context(), id, 2000)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, logs)
		})

		r.Post("/{id}/cancel", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if err := orchestrator.CancelRun(req.Context(), id, "tui"); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		r.Get("/{id}/stream", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			detail, err := orchestrator.GetRun(req.Context(), id)
			if err != nil {
				writeError(w, http.StatusNotFound, err)
				return
			}
			orchestrator.Streams.Ensure(core.RunStreamID(id))
			runEvents, err := orchestrator.Streams.Subscribe(req.Context(), core.RunStreamID(id))
			if err != nil {
				writeError(w, http.StatusNotFound, err)
				return
			}
			channels := []<-chan stream.Event{runEvents}
			if tailer != nil {
				for _, hostRun := range detail.HostRuns {
					if hostRun.LogPath == "" {
						continue
					}
					logEvents, err := tailer.Subscribe(req.Context(), stream.TailTarget{RunID: id, HostRunID: hostRun.ID, HostID: hostRun.HostID, HostName: hostRun.HostName, Path: hostRun.LogPath})
					if err == nil {
						channels = append(channels, logEvents)
					}
				}
			}
			serveSSEMany(req.Context(), w, channels)
		})
	})
}

func runListQuery(req *http.Request) core.RunListQuery {
	query := req.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	offset, _ := strconv.Atoi(query.Get("offset"))
	result := core.RunListQuery{Limit: limit, Offset: offset}
	for _, value := range splitCSV(query.Get("status")) {
		result.Statuses = append(result.Statuses, core.RunStatus(value))
	}
	for _, value := range splitCSV(query.Get("trigger")) {
		result.Triggers = append(result.Triggers, core.RunTrigger(value))
	}
	switch query.Get("range") {
	case "24h":
		since := time.Now().UTC().Add(-24 * time.Hour)
		result.Since = &since
	case "7d":
		since := time.Now().UTC().Add(-7 * 24 * time.Hour)
		result.Since = &since
	case "30d":
		since := time.Now().UTC().Add(-30 * 24 * time.Hour)
		result.Since = &since
	}
	return result
}

func splitCSV(value string) []string {
	parts := []string{}
	for _, part := range strings.Split(value, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func serveSSEMany(ctx context.Context, w http.ResponseWriter, channels []<-chan stream.Event) {
	merged := make(chan stream.Event, 64)
	for _, events := range channels {
		events := events
		go func() {
			for event := range events {
				select {
				case merged <- event:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	serveSSE(ctx, w, merged)
}
