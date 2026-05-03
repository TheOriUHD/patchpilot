package store

import (
	"context"
	"testing"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/migrations"
)

func TestScheduleRepositoryCRUD(t *testing.T) {
	t.Parallel()
	db, err := Open(context.Background(), t.TempDir()+"/patchpilot.db")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background(), migrations.Files); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	hostRepo := NewHostRepository(db)
	host, err := hostRepo.Create(context.Background(), core.HostCreate{Name: "h1", Hostname: "192.0.2.1", SSHUser: "patchpilot", Type: core.HostTypeStandard, PackageManager: "apt", Enabled: true})
	if err != nil {
		t.Fatalf("Create host error: %v", err)
	}
	repo := NewScheduleRepository(db)
	next := time.Now().UTC().Add(time.Hour)
	schedule, err := repo.Create(context.Background(), core.ScheduleInput{Name: "weekly", Frequency: core.FrequencyWeekly, Minute: 0, Hour: 3, DayOfWeek: 0, DayOfMonth: 1, Enabled: true, HostIDs: []int64{host.ID}}, &next)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if schedule.ID == 0 || schedule.HostCount != 1 || len(schedule.HostIDs) != 1 {
		t.Fatalf("created schedule = %#v", schedule)
	}

	schedule, err = repo.Update(context.Background(), schedule.ID, core.ScheduleInput{Name: "daily", Frequency: core.FrequencyDaily, Minute: 30, Hour: 4, Enabled: false}, &next)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if schedule.Name != "daily" || schedule.Enabled || schedule.HostCount != 0 {
		t.Fatalf("updated schedule = %#v", schedule)
	}
	if err := repo.MarkSkipped(context.Background(), schedule.ID, time.Now().UTC(), "overlap", &next); err != nil {
		t.Fatalf("MarkSkipped() error: %v", err)
	}
	schedule, err = repo.Get(context.Background(), schedule.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if schedule.LastSkippedAt == nil || schedule.LastSkipReason != "overlap" {
		t.Fatalf("skip metadata = %#v", schedule)
	}
	if err := repo.Delete(context.Background(), schedule.ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}
