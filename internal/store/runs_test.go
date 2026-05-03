package store

import (
	"context"
	"testing"

	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/migrations"
)

func TestRunRepositoryQueueAndPromotion(t *testing.T) {
	t.Parallel()
	db, err := Open(context.Background(), t.TempDir()+"/patchpilot.db")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background(), migrations.Files); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	host, err := NewHostRepository(db).Create(context.Background(), core.HostCreate{Name: "h1", Hostname: "192.0.2.1", SSHUser: "patchpilot", Type: core.HostTypeStandard, PackageManager: "apt", Enabled: true})
	if err != nil {
		t.Fatalf("Create host error: %v", err)
	}
	repo := NewRunRepository(db, t.TempDir())
	detail, err := repo.CreateRunWithHosts(context.Background(), core.RunCreate{Trigger: core.RunTriggerManual, Status: core.RunStatusQueued, Summary: "queued"}, []int64{host.ID})
	if err != nil {
		t.Fatalf("CreateRunWithHosts() error: %v", err)
	}
	if detail.Run.Status != core.RunStatusQueued || len(detail.HostRuns) != 1 {
		t.Fatalf("detail = %#v", detail)
	}
	next, err := repo.NextQueued(context.Background())
	if err != nil {
		t.Fatalf("NextQueued() error: %v", err)
	}
	if next == nil || next.Run.ID != detail.Run.ID {
		t.Fatalf("NextQueued() = %#v", next)
	}
	promoted, err := repo.PromoteQueued(context.Background(), detail.Run.ID)
	if err != nil {
		t.Fatalf("PromoteQueued() error: %v", err)
	}
	if promoted.Run.Status != core.RunStatusRunning {
		t.Fatalf("promoted status = %q", promoted.Run.Status)
	}
}
