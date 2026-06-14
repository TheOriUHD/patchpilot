package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/store"
	"github.com/TheOriUHD/patchpilot/internal/stream"
	"github.com/TheOriUHD/patchpilot/migrations"
)

func TestHostMutationRejectedWhileHostRunInFlight(t *testing.T) {
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

	hostRepo := store.NewHostRepository(db)
	host, err := hostRepo.Create(ctx, core.HostCreate{Name: "debian", Hostname: "192.0.2.10", SSHUser: "patchpilot", Type: core.HostTypeStandard, PackageManager: "apt", Enabled: true})
	if err != nil {
		t.Fatalf("Create host error: %v", err)
	}
	runRepo := store.NewRunRepository(db, t.TempDir())
	if _, err := runRepo.CreateRunWithHosts(ctx, core.RunCreate{Trigger: core.RunTriggerManual, Status: core.RunStatusRunning, Summary: "test"}, []int64{host.ID}); err != nil {
		t.Fatalf("Create run error: %v", err)
	}

	service := core.NewHostService(hostRepo, nil, nil, stream.NewHub(), slog.Default())
	orchestrator := core.NewOrchestrator(hostRepo, runRepo, nil, nil, nil, stream.NewHub(), slog.Default())
	router := chi.NewRouter()
	RegisterHosts(router, service, orchestrator)

	request := httptest.NewRequest(http.MethodDelete, "/api/hosts/1", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "cannot remove host while update is in progress") {
		t.Fatalf("body = %q, want in-flight rejection", response.Body.String())
	}
}
