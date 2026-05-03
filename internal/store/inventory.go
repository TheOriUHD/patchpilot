package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/TheOriUHD/patchpilot/internal/core"
)

// InventoryRepository exports and imports inventory backups.
type InventoryRepository struct {
	db        *sql.DB
	hosts     *HostRepository
	schedules *ScheduleRepository
	settings  *SettingsRepository
}

// NewInventoryRepository creates a SQLite-backed inventory repository.
func NewInventoryRepository(db *DB) *InventoryRepository {
	return &InventoryRepository{db: db.SQLDB(), hosts: NewHostRepository(db), schedules: NewScheduleRepository(db), settings: NewSettingsRepository(db)}
}

// Export returns the token-free inventory backup.
func (r *InventoryRepository) Export(ctx context.Context) (core.InventoryBackup, error) {
	hosts, err := r.hosts.List(ctx)
	if err != nil {
		return core.InventoryBackup{}, err
	}
	schedules, err := r.schedules.List(ctx)
	if err != nil {
		return core.InventoryBackup{}, err
	}
	links, err := r.scheduleHosts(ctx)
	if err != nil {
		return core.InventoryBackup{}, err
	}
	concurrency, err := r.settings.ConcurrencyEnabled(ctx)
	if err != nil {
		return core.InventoryBackup{}, err
	}
	backup := core.InventoryBackup{FormatVersion: 1, ConcurrencyEnabled: concurrency, ScheduleHosts: links}
	for _, host := range hosts {
		backup.Hosts = append(backup.Hosts, core.InventoryHost{ID: host.ID, Name: host.Name, Hostname: host.Hostname, SSHPort: host.SSHPort, SSHUser: host.SSHUser, Type: host.Type, PackageManager: host.PackageManager, OSPretty: host.OSPretty, Arch: host.Arch, IsProxmoxHost: host.IsProxmoxHost, IsSelf: host.IsSelf, Enabled: host.Enabled, Notes: host.Notes})
	}
	for _, schedule := range schedules {
		backup.Schedules = append(backup.Schedules, core.InventorySchedule{ID: schedule.ID, Name: schedule.Name, Frequency: schedule.Frequency, Minute: schedule.Minute, Hour: schedule.Hour, DayOfWeek: schedule.DayOfWeek, DayOfMonth: schedule.DayOfMonth, Enabled: schedule.Enabled, MaxConcurrent: schedule.MaxConcurrent})
	}
	return backup, nil
}

// Import applies a token-free inventory backup.
func (r *InventoryRepository) Import(ctx context.Context, backup core.InventoryBackup, options core.InventoryImportOptions) (core.InventoryImportResult, error) {
	if backup.FormatVersion != 1 {
		return core.InventoryImportResult{}, fmt.Errorf("unsupported inventory format_version %d", backup.FormatVersion)
	}
	if options.Conflicts == nil {
		options.Conflicts = map[string]core.InventoryConflictResolution{}
	}
	existingHosts, err := r.hosts.List(ctx)
	if err != nil {
		return core.InventoryImportResult{}, err
	}
	existingSchedules, err := r.schedules.List(ctx)
	if err != nil {
		return core.InventoryImportResult{}, err
	}
	existingByName := map[string]core.Host{}
	for _, host := range existingHosts {
		existingByName[strings.ToLower(host.Name)] = host
	}
	existingScheduleByName := map[string]core.Schedule{}
	for _, schedule := range existingSchedules {
		existingScheduleByName[strings.ToLower(schedule.Name)] = schedule
	}
	result := core.InventoryImportResult{}
	for _, host := range backup.Hosts {
		if _, ok := existingByName[strings.ToLower(host.Name)]; ok {
			if _, hasResolution := options.Conflicts[host.Name]; !hasResolution {
				result.Conflicts = append(result.Conflicts, host.Name)
			}
		}
	}
	for _, schedule := range backup.Schedules {
		if _, ok := existingScheduleByName[strings.ToLower(schedule.Name)]; ok {
			if _, hasResolution := options.Conflicts[schedule.Name]; !hasResolution {
				result.Conflicts = append(result.Conflicts, schedule.Name)
			}
		}
	}
	if len(result.Conflicts) > 0 {
		return result, nil
	}
	idMap := map[int64]int64{}
	for _, host := range backup.Hosts {
		name := host.Name
		if existing, ok := existingByName[strings.ToLower(name)]; ok {
			resolution, hasResolution := options.Conflicts[host.Name]
			if !hasResolution {
				result.Conflicts = append(result.Conflicts, host.Name)
				idMap[host.ID] = existing.ID
				continue
			}
			switch resolution.Action {
			case core.InventoryConflictSkip:
				idMap[host.ID] = existing.ID
				continue
			case core.InventoryConflictOverwrite:
				if err := r.overwriteHost(ctx, existing.ID, host); err != nil {
					return result, err
				}
				idMap[host.ID] = existing.ID
				result.ImportedHosts++
				continue
			case core.InventoryConflictRename:
				if strings.TrimSpace(resolution.NewName) == "" {
					return result, fmt.Errorf("rename conflict for %s requires new_name", host.Name)
				}
				name = strings.TrimSpace(resolution.NewName)
			default:
				return result, fmt.Errorf("unsupported conflict action %q", resolution.Action)
			}
		}
		created, err := r.hosts.Create(ctx, core.HostCreate{Name: name, Hostname: host.Hostname, SSHPort: host.SSHPort, SSHUser: host.SSHUser, Type: host.Type, PackageManager: host.PackageManager, OSPretty: host.OSPretty, Arch: host.Arch, IsProxmoxHost: host.IsProxmoxHost, IsSelf: host.IsSelf, Enabled: host.Enabled, Notes: host.Notes})
		if err != nil {
			return result, err
		}
		idMap[host.ID] = created.ID
		existingByName[strings.ToLower(name)] = created
		result.ImportedHosts++
	}
	if err := r.settings.SetBool(ctx, core.SettingConcurrencyEnabled, backup.ConcurrencyEnabled); err != nil {
		return result, err
	}
	for _, schedule := range backup.Schedules {
		hostIDs := linkedHostIDs(backup.ScheduleHosts, schedule.ID, idMap)
		input := core.ScheduleInput{Name: schedule.Name, Frequency: schedule.Frequency, Minute: schedule.Minute, Hour: schedule.Hour, DayOfWeek: schedule.DayOfWeek, DayOfMonth: schedule.DayOfMonth, Enabled: schedule.Enabled, MaxConcurrent: schedule.MaxConcurrent, HostIDs: hostIDs}
		if existing, ok := existingScheduleByName[strings.ToLower(schedule.Name)]; ok {
			resolution := options.Conflicts[schedule.Name]
			switch resolution.Action {
			case core.InventoryConflictSkip:
				continue
			case core.InventoryConflictOverwrite:
				if _, err := r.schedules.Update(ctx, existing.ID, input, nil); err != nil {
					return result, err
				}
				result.ImportedSchedules++
				continue
			case core.InventoryConflictRename:
				if strings.TrimSpace(resolution.NewName) == "" {
					return result, fmt.Errorf("rename conflict for %s requires new_name", schedule.Name)
				}
				input.Name = strings.TrimSpace(resolution.NewName)
			default:
				return result, fmt.Errorf("unsupported conflict action %q", resolution.Action)
			}
		}
		created, err := r.schedules.Create(ctx, input, nil)
		if err != nil {
			return result, err
		}
		existingScheduleByName[strings.ToLower(created.Name)] = created
		result.ImportedSchedules++
	}
	return result, nil
}

func (r *InventoryRepository) scheduleHosts(ctx context.Context) ([]core.InventoryScheduleHost, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT schedule_id, host_id, order_index FROM schedule_hosts ORDER BY schedule_id, order_index`)
	if err != nil {
		return nil, fmt.Errorf("list schedule host links: %w", err)
	}
	defer rows.Close()
	links := []core.InventoryScheduleHost{}
	for rows.Next() {
		var link core.InventoryScheduleHost
		if err := rows.Scan(&link.ScheduleID, &link.HostID, &link.OrderIndex); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return links, nil
}

func (r *InventoryRepository) overwriteHost(ctx context.Context, id int64, host core.InventoryHost) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE hosts
		SET hostname = ?, ssh_port = ?, ssh_user = ?, host_type = ?, package_manager = NULLIF(?, ''), os_pretty = NULLIF(?, ''), arch = NULLIF(?, ''), is_proxmox_host = ?, is_self = ?, enabled = ?, notes = ?
		WHERE id = ?
	`, host.Hostname, host.SSHPort, host.SSHUser, host.Type, host.PackageManager, host.OSPretty, host.Arch, boolInt(host.IsProxmoxHost), boolInt(host.IsSelf), boolInt(host.Enabled), host.Notes, id)
	if err != nil {
		return fmt.Errorf("overwrite host %s: %w", host.Name, err)
	}
	return nil
}

func linkedHostIDs(links []core.InventoryScheduleHost, scheduleID int64, idMap map[int64]int64) []int64 {
	hostIDs := []int64{}
	for _, link := range links {
		if link.ScheduleID != scheduleID {
			continue
		}
		if newID, ok := idMap[link.HostID]; ok {
			hostIDs = append(hostIDs, newID)
		}
	}
	return hostIDs
}
