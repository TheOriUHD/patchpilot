package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestApplyUpdateSuccessWithProgress(t *testing.T) {
	t.Parallel()
	jobPolls := 0
	updateCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.String() {
		case "/api/v2.0/system/info":
			writeJSON(t, w, map[string]any{"version": "TrueNAS-SCALE-24.10.1", "product": "SCALE"})
		case "/api/v2.0/update/check_available":
			writeJSON(t, w, map[string]any{"status": "AVAILABLE", "changelog": "fixes"})
		case "/api/v2.0/update/update":
			updateCalled = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			if body["reboot"] != false {
				t.Fatalf("update reboot = %v, want false", body["reboot"])
			}
			writeJSON(t, w, 42)
		case "/api/v2.0/core/get_jobs?id=42":
			jobPolls++
			if jobPolls == 1 {
				writeJSON(t, w, []map[string]any{{"id": 42, "state": "RUNNING", "progress": map[string]any{"percent": 50, "description": "installing"}}})
				return
			}
			writeJSON(t, w, []map[string]any{{"id": 42, "state": "SUCCESS", "progress": map[string]any{"percent": 100, "description": "complete"}, "result": map[string]any{"reboot_required": false}}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	client.PollInterval = time.Millisecond
	info, err := client.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}
	if info.Version != "TrueNAS-SCALE-24.10.1" {
		t.Fatalf("version = %q", info.Version)
	}
	var logs []string
	result, err := client.ApplyUpdate(context.Background(), func(line string) { logs = append(logs, line) })
	if err != nil {
		t.Fatalf("ApplyUpdate() error: %v", err)
	}
	if !updateCalled || !result.Applied || result.RebootRequired {
		t.Fatalf("result = %#v updateCalled=%v", result, updateCalled)
	}
	if !strings.Contains(strings.Join(logs, ""), "50%") {
		t.Fatalf("logs missing progress: %q", logs)
	}
}

func TestApplyUpdateUnavailableDoesNotStartJob(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2.0/update/check_available":
			writeJSON(t, w, map[string]any{"status": "UNAVAILABLE"})
		case "/api/v2.0/update/update":
			t.Fatalf("update endpoint should not be called")
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	result, err := client.ApplyUpdate(context.Background(), nil)
	if err != nil {
		t.Fatalf("ApplyUpdate() error: %v", err)
	}
	if result.Applied || result.Summary != "no updates available" {
		t.Fatalf("result = %#v", result)
	}
}

func TestApplyUpdateRebootRequired(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/v2.0/update/check_available":
			writeJSON(t, w, map[string]any{"status": "AVAILABLE"})
		case "/api/v2.0/update/update":
			writeJSON(t, w, map[string]any{"job_id": 7})
		case "/api/v2.0/core/get_jobs?id=7":
			writeJSON(t, w, []map[string]any{{"id": 7, "state": "SUCCESS", "result": map[string]any{"reboot_required": true}}})
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	client.PollInterval = time.Millisecond
	result, err := client.ApplyUpdate(context.Background(), nil)
	if err != nil {
		t.Fatalf("ApplyUpdate() error: %v", err)
	}
	if !result.RebootRequired {
		t.Fatalf("RebootRequired = false, want true")
	}
}

func TestVerifyRefusesCore(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"version": "TrueNAS-13.0-U6", "product": "CORE"})
	}))
	defer server.Close()

	_, err := New(server.URL, "secret").Verify(context.Background())
	if err == nil || err.Error() != CoreUnsupportedMessage {
		t.Fatalf("Verify() error = %v, want Core refusal", err)
	}
}

func TestVerifyRefusesHostTypeMismatch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"result": "ok", "data": map[string]any{"version": "2024.12"}})
	}))
	defer server.Close()

	_, err := New(server.URL, "secret").Verify(context.Background())
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("Verify() error = %v, want ErrAPI", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
