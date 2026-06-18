package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/store"
	"github.com/TheOriUHD/patchpilot/migrations"
)

func TestScheduleHandlersRoundTripSuppressReboot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir()+"/patchpilot.db")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(ctx, migrations.Files); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	schedules := store.NewScheduleRepository(db)
	orchestrator := core.NewOrchestrator(nil, nil, schedules, nil, nil, nil, nil)
	router := chi.NewRouter()
	RegisterSchedules(router, orchestrator, nil)

	created := requestSchedule(t, router, http.MethodPost, "/api/schedules/", core.ScheduleInput{Name: "weekly", Frequency: core.FrequencyWeekly, Minute: 0, Hour: 3, DayOfWeek: 0, DayOfMonth: 1, Enabled: true, SuppressReboot: true})
	if !created.SuppressReboot {
		t.Fatalf("created schedule suppress_reboot = false, want true")
	}
	updated := requestSchedule(t, router, http.MethodPut, "/api/schedules/1", core.ScheduleInput{Name: "weekly", Frequency: core.FrequencyWeekly, Minute: 0, Hour: 3, DayOfWeek: 0, DayOfMonth: 1, Enabled: true, SuppressReboot: false})
	if updated.SuppressReboot {
		t.Fatalf("updated schedule suppress_reboot = true, want false")
	}
}

func requestSchedule(t *testing.T, router http.Handler, method string, path string, input core.ScheduleInput) core.Schedule {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code < 200 || response.Code > 299 {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var schedule core.Schedule
	if err := json.Unmarshal(response.Body.Bytes(), &schedule); err != nil {
		t.Fatalf("Unmarshal() error: %v; body=%s", err, response.Body.String())
	}
	return schedule
}
