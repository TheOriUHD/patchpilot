package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/core"
)

// HostRepository persists host inventory records in SQLite.
type HostRepository struct {
	db *sql.DB
}

// NewHostRepository creates a SQLite-backed host repository.
func NewHostRepository(db *DB) *HostRepository {
	return &HostRepository{db: db.SQLDB()}
}

// List returns all hosts ordered by name.
func (r *HostRepository) List(ctx context.Context) ([]core.Host, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, name, hostname, ssh_port, ssh_user, host_type, COALESCE(package_manager, ''),
               COALESCE(os_pretty, ''), COALESCE(arch, ''), is_proxmox_host, is_self, enabled,
               COALESCE(notes, ''), last_seen_at, created_at, updated_at
        FROM hosts
        ORDER BY name COLLATE NOCASE
    `)
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	defer rows.Close()

	hosts := make([]core.Host, 0)
	for rows.Next() {
		host, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, host)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hosts: %w", err)
	}
	return hosts, nil
}

// Get returns one host by id.
func (r *HostRepository) Get(ctx context.Context, id int64) (core.Host, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT id, name, hostname, ssh_port, ssh_user, host_type, COALESCE(package_manager, ''),
               COALESCE(os_pretty, ''), COALESCE(arch, ''), is_proxmox_host, is_self, enabled,
               COALESCE(notes, ''), last_seen_at, created_at, updated_at
        FROM hosts
        WHERE id = ?
    `, id)
	host, err := scanHost(row)
	if err != nil {
		return core.Host{}, fmt.Errorf("get host %d: %w", id, err)
	}
	return host, nil
}

// Create inserts a host and returns the persisted record.
func (r *HostRepository) Create(ctx context.Context, input core.HostCreate) (core.Host, error) {
	port := input.SSHPort
	if port == 0 {
		port = 22
	}
	result, err := r.db.ExecContext(ctx, `
        INSERT INTO hosts (name, hostname, ssh_port, ssh_user, host_type, package_manager, os_pretty, arch, is_proxmox_host, is_self, api_token, api_token_nonce, enabled, notes, last_seen_at)
        VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, CURRENT_TIMESTAMP)
    `, input.Name, input.Hostname, port, input.SSHUser, input.Type, input.PackageManager, input.OSPretty, input.Arch, boolInt(input.IsProxmoxHost), boolInt(input.IsSelf), input.APITokenCiphertext, input.APITokenNonce, boolInt(input.Enabled), input.Notes)
	if err != nil {
		return core.Host{}, fmt.Errorf("create host: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return core.Host{}, fmt.Errorf("created host id: %w", err)
	}
	return r.Get(ctx, id)
}

// GetAPIToken returns the encrypted API token for an API-managed host.
func (r *HostRepository) GetAPIToken(ctx context.Context, id int64) (core.EncryptedSecret, error) {
	var token core.EncryptedSecret
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(api_token, ''), COALESCE(api_token_nonce, '') FROM hosts WHERE id = ?`, id).Scan(&token.Ciphertext, &token.Nonce); err != nil {
		return core.EncryptedSecret{}, fmt.Errorf("get host %d api token: %w", id, err)
	}
	if token.Ciphertext == "" || token.Nonce == "" {
		return core.EncryptedSecret{}, fmt.Errorf("host %d has no stored API token", id)
	}
	return token, nil
}

// Update mutates host display fields.
func (r *HostRepository) Update(ctx context.Context, id int64, input core.HostUpdate) (core.Host, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE hosts SET name = ?, enabled = ?, notes = ? WHERE id = ?`, input.Name, boolInt(input.Enabled), input.Notes, id); err != nil {
		return core.Host{}, fmt.Errorf("update host %d: %w", id, err)
	}
	return r.Get(ctx, id)
}

// SetEnabled enables or disables a host.
func (r *HostRepository) SetEnabled(ctx context.Context, id int64, enabled bool) (core.Host, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE hosts SET enabled = ? WHERE id = ?`, boolInt(enabled), id); err != nil {
		return core.Host{}, fmt.Errorf("set host %d enabled: %w", id, err)
	}
	return r.Get(ctx, id)
}

// Delete removes a host from inventory.
func (r *HostRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM hosts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete host %d: %w", id, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted host %d: %w", id, err)
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type hostScanner interface {
	Scan(dest ...any) error
}

func scanHost(scanner hostScanner) (core.Host, error) {
	var host core.Host
	var hostType string
	var lastSeen sql.NullTime
	var created time.Time
	var updated time.Time
	var isProxmox int
	var isSelf int
	var enabled int

	if err := scanner.Scan(&host.ID, &host.Name, &host.Hostname, &host.SSHPort, &host.SSHUser, &hostType, &host.PackageManager, &host.OSPretty, &host.Arch, &isProxmox, &isSelf, &enabled, &host.Notes, &lastSeen, &created, &updated); err != nil {
		return core.Host{}, err
	}
	host.Type = core.HostType(hostType)
	host.IsProxmoxHost = isProxmox == 1
	host.IsSelf = isSelf == 1
	host.Enabled = enabled == 1
	if lastSeen.Valid {
		host.LastSeenAt = &lastSeen.Time
	}
	host.CreatedAt = created
	host.UpdatedAt = updated
	return host, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
