package store

import (
	"context"
	"strings"
	"testing"

	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/migrations"
	"gopkg.in/yaml.v3"
)

func TestInventoryRoundTripExcludesAPITokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := openMigratedTestDB(t)
	defer func() { _ = source.Close() }()
	hosts := NewHostRepository(source)
	standard, err := hosts.Create(ctx, core.HostCreate{Name: "vm1", Hostname: "192.0.2.10", SSHUser: "patchpilot", Type: core.HostTypeStandard, PackageManager: "apt", Enabled: true})
	if err != nil {
		t.Fatalf("Create standard host error: %v", err)
	}
	_, err = hosts.Create(ctx, core.HostCreate{Name: "nas", Hostname: "truenas.local", Type: core.HostTypeTrueNAS, PackageManager: "truenas_api", Enabled: true, APITokenCiphertext: "cipher-secret", APITokenNonce: "nonce-secret"})
	if err != nil {
		t.Fatalf("Create API host error: %v", err)
	}
	settings := NewSettingsRepository(source)
	if err := settings.SetBool(ctx, core.SettingConcurrencyEnabled, true); err != nil {
		t.Fatalf("SetBool() error: %v", err)
	}
	schedules := NewScheduleRepository(source)
	if _, err := schedules.Create(ctx, core.ScheduleInput{Name: "weekly", Frequency: core.FrequencyWeekly, Hour: 3, DayOfWeek: 0, Enabled: true, MaxConcurrent: 4, SuppressReboot: true, HostIDs: []int64{standard.ID}}, nil); err != nil {
		t.Fatalf("Create schedule error: %v", err)
	}
	backup, err := NewInventoryRepository(source).Export(ctx)
	if err != nil {
		t.Fatalf("Export() error: %v", err)
	}
	if backup.FormatVersion != 1 || !backup.ConcurrencyEnabled {
		t.Fatalf("backup metadata = %#v", backup)
	}
	raw, err := yaml.Marshal(backup)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	serialized := string(raw)
	for _, forbidden := range []string{"cipher-secret", "nonce-secret", "api_token"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("backup leaked %q: %s", forbidden, serialized)
		}
	}
	target := openMigratedTestDB(t)
	defer func() { _ = target.Close() }()
	result, err := NewInventoryRepository(target).Import(ctx, backup, core.InventoryImportOptions{})
	if err != nil {
		t.Fatalf("Import() error: %v", err)
	}
	if result.ImportedHosts != 2 || result.ImportedSchedules != 1 || len(result.Conflicts) != 0 {
		t.Fatalf("import result = %#v", result)
	}
	importedHosts, err := NewHostRepository(target).List(ctx)
	if err != nil {
		t.Fatalf("List hosts error: %v", err)
	}
	if len(importedHosts) != 2 {
		t.Fatalf("imported host count = %d, want 2", len(importedHosts))
	}
	importedSchedules, err := NewScheduleRepository(target).List(ctx)
	if err != nil {
		t.Fatalf("List schedules error: %v", err)
	}
	if len(importedSchedules) != 1 || importedSchedules[0].MaxConcurrent != 4 || !importedSchedules[0].SuppressReboot || importedSchedules[0].HostCount != 1 {
		t.Fatalf("imported schedules = %#v", importedSchedules)
	}
	if enabled, err := NewSettingsRepository(target).ConcurrencyEnabled(ctx); err != nil || !enabled {
		t.Fatalf("imported concurrency = %v, %v", enabled, err)
	}
	for _, host := range importedHosts {
		if host.Type == core.HostTypeTrueNAS {
			if _, err := NewHostRepository(target).GetAPIToken(ctx, host.ID); err == nil {
				t.Fatalf("imported API token unexpectedly exists")
			}
		}
	}
}

func TestInventoryImportConflictPreflight(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openMigratedTestDB(t)
	defer func() { _ = db.Close() }()
	if _, err := NewHostRepository(db).Create(ctx, core.HostCreate{Name: "vm1", Hostname: "old", SSHUser: "patchpilot", Type: core.HostTypeStandard, PackageManager: "apt", Enabled: true}); err != nil {
		t.Fatalf("Create host error: %v", err)
	}
	backup := core.InventoryBackup{FormatVersion: 1, Hosts: []core.InventoryHost{{ID: 10, Name: "vm1", Hostname: "new", SSHUser: "patchpilot", Type: core.HostTypeStandard, PackageManager: "apt", Enabled: true}, {ID: 11, Name: "vm2", Hostname: "new2", SSHUser: "patchpilot", Type: core.HostTypeStandard, PackageManager: "apt", Enabled: true}}}
	result, err := NewInventoryRepository(db).Import(ctx, backup, core.InventoryImportOptions{})
	if err != nil {
		t.Fatalf("Import() error: %v", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0] != "vm1" {
		t.Fatalf("conflicts = %#v", result.Conflicts)
	}
	hosts, err := NewHostRepository(db).List(ctx)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(hosts) != 1 || hosts[0].Hostname != "old" {
		t.Fatalf("import mutated before conflict resolution: %#v", hosts)
	}
}

func openMigratedTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), t.TempDir()+"/patchpilot.db")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	if err := db.Migrate(context.Background(), migrations.Files); err != nil {
		_ = db.Close()
		t.Fatalf("Migrate() error: %v", err)
	}
	return db
}
