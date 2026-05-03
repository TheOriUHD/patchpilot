package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/scheduler"
)

// RegisterSchedules mounts schedule CRUD endpoints.
func RegisterSchedules(router chi.Router, orchestrator *core.Orchestrator, engine *scheduler.Engine) {
	router.Route("/api/schedules", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			schedules, err := orchestrator.Schedules.List(req.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, schedules)
		})

		r.Post("/", func(w http.ResponseWriter, req *http.Request) {
			var input core.ScheduleInput
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			input.Name = strings.TrimSpace(input.Name)
			nextRunAt, err := nextRunForInput(input)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			schedule, err := orchestrator.Schedules.Create(req.Context(), input, nextRunAt)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			orchestrator.RecordAudit(req.Context(), "tui", "schedule.create", "schedule", &schedule.ID, map[string]any{"name": schedule.Name})
			reloadSchedules(req.Context(), engine)
			writeJSON(w, http.StatusCreated, schedule)
		})

		r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			schedule, err := orchestrator.Schedules.Get(req.Context(), id)
			if err != nil {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeJSON(w, http.StatusOK, schedule)
		})

		r.Put("/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			var input core.ScheduleInput
			if err := readJSON(req, &input); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			input.Name = strings.TrimSpace(input.Name)
			nextRunAt, err := nextRunForInput(input)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			schedule, err := orchestrator.Schedules.Update(req.Context(), id, input, nextRunAt)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			orchestrator.RecordAudit(req.Context(), "tui", "schedule.update", "schedule", &schedule.ID, map[string]any{"name": schedule.Name})
			reloadSchedules(req.Context(), engine)
			writeJSON(w, http.StatusOK, schedule)
		})

		r.Delete("/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if err := orchestrator.Schedules.Delete(req.Context(), id); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			orchestrator.RecordAudit(req.Context(), "tui", "schedule.delete", "schedule", &id, map[string]any{})
			reloadSchedules(req.Context(), engine)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		r.Post("/{id}/run", func(w http.ResponseWriter, req *http.Request) {
			id, err := idParam(req, "id")
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			detail, err := orchestrator.RunSchedule(req.Context(), id, core.RunTriggerManual, "tui", nil)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusAccepted, detail)
		})
	})
}

func nextRunForInput(input core.ScheduleInput) (*time.Time, error) {
	runs, err := scheduler.NextRuns(input, time.Now().UTC(), 1)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[0], nil
}

func reloadSchedules(ctx context.Context, engine *scheduler.Engine) {
	if engine == nil {
		return
	}
	go func() {
		requestCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if ctx.Err() != nil {
			return
		}
		_ = engine.Reload(requestCtx)
	}()
}
