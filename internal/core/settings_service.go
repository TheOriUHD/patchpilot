package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
	"github.com/TheOriUHD/patchpilot/internal/version"
	golangssh "golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

// SettingsRepository persists mutable daemon settings.
type SettingsRepository interface {
	GetBool(ctx context.Context, key string, fallback bool) (bool, error)
	SetBool(ctx context.Context, key string, value bool) error
	ConcurrencyEnabled(ctx context.Context) (bool, error)
}

// InventoryRepository exports/imports token-free inventory backups.
type InventoryRepository interface {
	Export(ctx context.Context) (InventoryBackup, error)
	Import(ctx context.Context, backup InventoryBackup, options InventoryImportOptions) (InventoryImportResult, error)
}

// SSHRunnerConnector opens SSH connections for key rotation.
type SSHRunnerConnector func(ctx context.Context, host string, port int, user string, signer golangssh.Signer) (remotessh.Runner, error)

// SettingsService coordinates settings, logs, key rotation, and backups.
type SettingsService struct {
	Hosts      HostRepository
	Settings   SettingsRepository
	Inventory  InventoryRepository
	Audit      AuditRepository
	Keys       *remotessh.KeyStore
	Paths      SettingsPaths
	LogPath    string
	Logger     *slog.Logger
	ConnectKey SSHRunnerConnector
}

// Snapshot returns the current settings view model.
func (s *SettingsService) Snapshot(ctx context.Context) (SettingsSnapshot, error) {
	concurrency, err := s.Settings.ConcurrencyEnabled(ctx)
	if err != nil {
		return SettingsSnapshot{}, err
	}
	keypair, err := s.Keys.Ensure(ctx)
	if err != nil {
		return SettingsSnapshot{}, err
	}
	info := version.Info()
	return SettingsSnapshot{
		Paths:              s.Paths,
		ConcurrencyEnabled: concurrency,
		PublicKey:          keypair.PublicKey,
		SelfUpdateLogs:     listSelfUpdateLogs(filepath.Dir(s.LogPath)),
		DaemonLogTail:      tailFile(s.LogPath, 80),
		About:              AboutInfo{Version: info.Version, Commit: info.Commit, BuildDate: info.BuildDate, GoVersion: runtime.Version(), OSArch: runtime.GOOS + "/" + runtime.GOARCH},
	}, nil
}

// SetConcurrency toggles experimental concurrency and audits the change.
func (s *SettingsService) SetConcurrency(ctx context.Context, enabled bool) error {
	old, err := s.Settings.ConcurrencyEnabled(ctx)
	if err != nil {
		return err
	}
	if err := s.Settings.SetBool(ctx, SettingConcurrencyEnabled, enabled); err != nil {
		return err
	}
	s.audit(ctx, "tui", "settings.concurrency_changed", "settings", nil, map[string]any{"old": old, "new": enabled})
	return nil
}

// ExportInventory writes a token-free YAML inventory backup.
func (s *SettingsService) ExportInventory(ctx context.Context, path string) error {
	backup, err := s.Inventory.Export(ctx)
	if err != nil {
		return err
	}
	raw, err := yaml.Marshal(backup)
	if err != nil {
		return fmt.Errorf("marshal inventory backup: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write inventory backup: %w", err)
	}
	s.audit(ctx, "tui", "inventory.export", "settings", nil, map[string]any{"path": path, "hosts": len(backup.Hosts), "schedules": len(backup.Schedules)})
	return nil
}

// ImportInventory reads and applies a token-free YAML inventory backup.
func (s *SettingsService) ImportInventory(ctx context.Context, input InventoryPathRequest) (InventoryImportResult, error) {
	raw, err := os.ReadFile(input.Path)
	if err != nil {
		return InventoryImportResult{}, fmt.Errorf("read inventory backup: %w", err)
	}
	var backup InventoryBackup
	if err := yaml.Unmarshal(raw, &backup); err != nil {
		return InventoryImportResult{}, fmt.Errorf("parse inventory backup: %w", err)
	}
	result, err := s.Inventory.Import(ctx, backup, InventoryImportOptions{Conflicts: input.Conflicts})
	if err != nil {
		s.audit(ctx, "tui", "inventory.import", "settings", nil, map[string]any{"path": input.Path, "error": err.Error()})
		return result, err
	}
	s.audit(ctx, "tui", "inventory.import", "settings", nil, map[string]any{"path": input.Path, "hosts": result.ImportedHosts, "schedules": result.ImportedSchedules, "conflicts": result.Conflicts})
	return result, nil
}

// RotateKeypair rotates the orchestrator SSH key across enabled Standard hosts.
func (s *SettingsService) RotateKeypair(ctx context.Context) error {
	hosts, err := s.Hosts.List(ctx)
	if err != nil {
		return err
	}
	targets := make([]Host, 0)
	for _, host := range hosts {
		if host.Enabled && host.Type == HostTypeStandard && !host.IsSelf {
			targets = append(targets, host)
		}
	}
	names := make([]string, 0, len(targets))
	for _, host := range targets {
		names = append(names, host.Name)
	}
	s.audit(ctx, "tui", "settings.key_rotation_start", "settings", nil, map[string]any{"hosts": names})
	oldKey, err := s.Keys.Ensure(ctx)
	if err != nil {
		return err
	}
	newKey, err := remotessh.GenerateInMemory()
	if err != nil {
		return err
	}
	connector := s.ConnectKey
	if connector == nil {
		connector = func(ctx context.Context, host string, port int, user string, signer golangssh.Signer) (remotessh.Runner, error) {
			return remotessh.ConnectKey(ctx, host, port, user, signer)
		}
	}
	installed := make([]Host, 0)
	failed := make([]string, 0)
	cleanupNew := func() {
		for _, host := range installed {
			runner, err := connector(context.Background(), host.Hostname, normalizePort(host.SSHPort), host.SSHUser, oldKey.Signer)
			if err == nil {
				_, _ = runner.Run(context.Background(), removeAuthorizedKeyCommand(newKey.PublicKey), nil)
				_ = runner.Close()
			}
		}
	}
	restoreOld := func(hosts []Host) {
		for _, host := range hosts {
			runner, err := connector(context.Background(), host.Hostname, normalizePort(host.SSHPort), host.SSHUser, newKey.Signer)
			if err == nil {
				_, _ = runner.Run(context.Background(), installAuthorizedKeyCommand(oldKey.PublicKey), nil)
				_ = runner.Close()
			}
		}
	}
	for _, host := range targets {
		runner, err := connector(ctx, host.Hostname, normalizePort(host.SSHPort), host.SSHUser, oldKey.Signer)
		if err != nil {
			failed = append(failed, host.Name+": "+err.Error())
			continue
		}
		if _, err := runner.Run(ctx, installAuthorizedKeyCommand(newKey.PublicKey), nil); err != nil {
			failed = append(failed, host.Name+": "+err.Error())
			_ = runner.Close()
			continue
		}
		_ = runner.Close()
		installed = append(installed, host)
		verify, err := connector(ctx, host.Hostname, normalizePort(host.SSHPort), host.SSHUser, newKey.Signer)
		if err != nil {
			failed = append(failed, host.Name+": "+err.Error())
			continue
		}
		_ = verify.Close()
	}
	if len(failed) > 0 {
		cleanupNew()
		s.audit(ctx, "tui", "settings.key_rotation_abort", "settings", nil, map[string]any{"failed": failed})
		return fmt.Errorf("key rotation aborted: %s", strings.Join(failed, "; "))
	}
	removedOld := make([]Host, 0, len(targets))
	for _, host := range targets {
		runner, err := connector(ctx, host.Hostname, normalizePort(host.SSHPort), host.SSHUser, oldKey.Signer)
		if err != nil {
			restoreOld(removedOld)
			cleanupNew()
			s.audit(ctx, "tui", "settings.key_rotation_abort", "settings", nil, map[string]any{"failed": []string{host.Name + ": " + err.Error()}})
			return fmt.Errorf("key rotation aborted while removing old key from %s: %w", host.Name, err)
		}
		_, err = runner.Run(ctx, removeAuthorizedKeyCommand(oldKey.PublicKey), nil)
		_ = runner.Close()
		if err != nil {
			restoreOld(removedOld)
			cleanupNew()
			s.audit(ctx, "tui", "settings.key_rotation_abort", "settings", nil, map[string]any{"failed": []string{host.Name + ": " + err.Error()}})
			return fmt.Errorf("key rotation aborted while removing old key from %s: %w", host.Name, err)
		}
		removedOld = append(removedOld, host)
	}
	if _, err := s.Keys.Replace(ctx, newKey); err != nil {
		restoreOld(removedOld)
		cleanupNew()
		s.audit(ctx, "tui", "settings.key_rotation_abort", "settings", nil, map[string]any{"failed": []string{err.Error()}})
		return err
	}
	s.audit(ctx, "tui", "settings.key_rotation_success", "settings", nil, map[string]any{"hosts": names})
	return nil
}

func installAuthorizedKeyCommand(publicKey string) string {
	key := strings.TrimSpace(publicKey)
	return "mkdir -p /home/patchpilot/.ssh && touch /home/patchpilot/.ssh/authorized_keys && chmod 0600 /home/patchpilot/.ssh/authorized_keys && grep -qxF " + shellQuote(key) + " /home/patchpilot/.ssh/authorized_keys || printf '%s\\n' " + shellQuote(key) + " >> /home/patchpilot/.ssh/authorized_keys"
}

func removeAuthorizedKeyCommand(publicKey string) string {
	key := strings.TrimSpace(publicKey)
	return "if test -f /home/patchpilot/.ssh/authorized_keys; then tmp=$(mktemp); grep -vxF " + shellQuote(key) + " /home/patchpilot/.ssh/authorized_keys > $tmp || true; cat $tmp > /home/patchpilot/.ssh/authorized_keys; rm -f $tmp; fi"
}

func listSelfUpdateLogs(logDir string) []SelfUpdateLog {
	matches, _ := filepath.Glob(filepath.Join(logDir, "self-update-*.log"))
	logs := make([]SelfUpdateLog, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		runID := int64(0)
		name := filepath.Base(path)
		trimmed := strings.TrimSuffix(strings.TrimPrefix(name, "self-update-"), ".log")
		if parsed, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			runID = parsed
		}
		logs = append(logs, SelfUpdateLog{RunID: runID, Path: path, Size: info.Size(), ModifiedAt: info.ModTime()})
	}
	sort.Slice(logs, func(left int, right int) bool { return logs[left].ModifiedAt.After(logs[right].ModifiedAt) })
	return logs
}

func tailFile(path string, maxLines int) []string {
	if maxLines <= 0 {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	lines := make([]string, 0, maxLines)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
		}
	}
	return lines
}

func (s *SettingsService) audit(ctx context.Context, actor string, action string, targetType string, targetID *int64, details map[string]any) {
	if s.Audit == nil {
		return
	}
	payload, err := json.Marshal(details)
	if err != nil {
		payload = []byte("{}")
	}
	if err := s.Audit.Create(ctx, AuditEvent{Actor: actor, Action: action, TargetType: targetType, TargetID: targetID, Details: string(payload)}); err != nil && s.Logger != nil {
		s.Logger.Error("create audit event", "error", err)
	}
}
