package store

import (
	"context"
	"testing"

	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/secret"
	"github.com/TheOriUHD/patchpilot/migrations"
)

func TestHostRepositoryCRUD(t *testing.T) {
	t.Parallel()

	db, err := Open(context.Background(), t.TempDir()+"/patchpilot.db")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(context.Background(), migrations.Files); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}

	repo := NewHostRepository(db)
	created, err := repo.Create(context.Background(), core.HostCreate{
		Name:           "serverph",
		Hostname:       "192.0.2.42",
		SSHPort:        22,
		SSHUser:        "patchpilot",
		Type:           core.HostTypeStandard,
		PackageManager: "apt",
		OSPretty:       "Debian GNU/Linux 12",
		Arch:           "x86_64",
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if created.ID == 0 || created.Name != "serverph" || !created.Enabled {
		t.Fatalf("created host = %#v", created)
	}

	listed, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List() len = %d, want 1", len(listed))
	}

	updated, err := repo.Update(context.Background(), created.ID, core.HostUpdate{Name: "serverph-renamed", Enabled: false, Notes: "quiet"})
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated.Name != "serverph-renamed" || updated.Enabled || updated.Notes != "quiet" {
		t.Fatalf("updated host = %#v", updated)
	}

	enabled, err := repo.SetEnabled(context.Background(), created.ID, true)
	if err != nil {
		t.Fatalf("SetEnabled() error: %v", err)
	}
	if !enabled.Enabled {
		t.Fatalf("enabled host = %#v", enabled)
	}

	if err := repo.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	listed, err = repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() after delete error: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("List() len after delete = %d, want 0", len(listed))
	}
}

func TestHostRepositoryAPICredentialRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/patchpilot.db")
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(ctx, migrations.Files); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	repo := NewHostRepository(db)
	host, err := repo.Create(ctx, core.HostCreate{Name: "nas", Hostname: "https://truenas.example", SSHUser: "api", Type: core.HostTypeTrueNAS, PackageManager: "truenas_api", Enabled: true})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	manager := secret.NewManager(t.TempDir() + "/secret.key")
	const tokenValue = "dummy-api-token"
	ciphertext, nonce, err := manager.EncryptString(ctx, tokenValue)
	if err != nil {
		t.Fatalf("EncryptString() error: %v", err)
	}
	updated, err := repo.SetAPICredential(ctx, host.ID, core.EncryptedSecret{Ciphertext: ciphertext, Nonce: nonce}, "root")
	if err != nil {
		t.Fatalf("SetAPICredential() error: %v", err)
	}
	if updated.APIUsername != "root" {
		t.Fatalf("APIUsername = %q, want root", updated.APIUsername)
	}
	stored, err := repo.GetAPIToken(ctx, host.ID)
	if err != nil {
		t.Fatalf("GetAPIToken() error: %v", err)
	}
	if stored.Ciphertext == tokenValue || stored.Nonce == tokenValue {
		t.Fatalf("stored token was not encrypted: %#v", stored)
	}
	decrypted, err := manager.DecryptString(ctx, stored.Ciphertext, stored.Nonce)
	if err != nil {
		t.Fatalf("DecryptString() error: %v", err)
	}
	if decrypted != tokenValue {
		t.Fatalf("decrypted token = %q, want %q", decrypted, tokenValue)
	}
}
