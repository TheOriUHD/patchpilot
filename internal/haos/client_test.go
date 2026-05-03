package haos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestApplyUpdatesSupervisorThenCore(t *testing.T) {
	t.Parallel()
	var order []string
	jobPolls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer llat" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/hassio/refresh_updates":
			writeJSON(t, w, map[string]any{"result": "ok"})
		case "/api/hassio/supervisor/info":
			writeJSON(t, w, map[string]any{"result": "ok", "data": map[string]any{"version": "2024.1", "version_latest": "2024.2"}})
		case "/api/hassio/core/info":
			writeJSON(t, w, map[string]any{"result": "ok", "data": map[string]any{"version": "2024.3", "version_latest": "2024.4"}})
		case "/api/hassio/supervisor/update":
			order = append(order, "supervisor")
			writeJSON(t, w, map[string]any{"result": "ok", "data": map[string]any{"job_id": "sup-job"}})
		case "/api/hassio/core/update":
			order = append(order, "core")
			writeJSON(t, w, map[string]any{"result": "ok", "data": map[string]any{"job_id": "core-job"}})
		case "/api/hassio/jobs/sup-job", "/api/hassio/jobs/core-job":
			jobID := r.URL.Path[stringsLastSlash(r.URL.Path)+1:]
			jobPolls[jobID]++
			writeJSON(t, w, map[string]any{"result": "ok", "data": map[string]any{"state": "SUCCESS"}})
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(server.URL, "llat")
	client.PollInterval = time.Millisecond
	result, err := client.ApplyUpdates(context.Background(), nil)
	if err != nil {
		t.Fatalf("ApplyUpdates() error: %v", err)
	}
	if !reflect.DeepEqual(order, []string{"supervisor", "core"}) {
		t.Fatalf("order = %#v", order)
	}
	if !reflect.DeepEqual(result.Updated, []string{"supervisor", "core"}) {
		t.Fatalf("updated = %#v", result.Updated)
	}
}

func TestCheckUpdatesUsesVersionComparison(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/hassio/refresh_updates":
			writeJSON(t, w, map[string]any{"result": "ok"})
		case "/api/hassio/supervisor/info":
			writeJSON(t, w, map[string]any{"result": "ok", "data": map[string]any{"version": "2024.1", "version_latest": "2024.2", "update_available": false}})
		case "/api/hassio/core/info":
			writeJSON(t, w, map[string]any{"result": "ok", "data": map[string]any{"version": "2024.3", "version_latest": "2024.3"}})
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()

	plan, err := New(server.URL, "llat").CheckUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdates() error: %v", err)
	}
	if !plan.Supervisor.UpdateAvailable {
		t.Fatalf("supervisor UpdateAvailable = false, want true")
	}
	if plan.Core.UpdateAvailable {
		t.Fatalf("core UpdateAvailable = true, want false")
	}
}

func TestVerifyRefusesHostTypeMismatch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"version": "TrueNAS-SCALE-24.10"})
	}))
	defer server.Close()

	_, err := New(server.URL, "llat").Verify(context.Background())
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("Verify() error = %v, want ErrAPI", err)
	}
}

func TestAuthFailure(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := New(server.URL, "bad").Verify(context.Background())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("Verify() error = %v, want ErrAuth", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func stringsLastSlash(value string) int {
	for index := len(value) - 1; index >= 0; index-- {
		if value[index] == '/' {
			return index
		}
	}
	return -1
}
