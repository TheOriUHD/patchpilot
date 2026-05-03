package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/TheOriUHD/patchpilot/internal/core"
)

// SettingsRepository persists daemon settings in SQLite.
type SettingsRepository struct {
	db *sql.DB
}

// NewSettingsRepository creates a SQLite-backed settings repository.
func NewSettingsRepository(db *DB) *SettingsRepository {
	return &SettingsRepository{db: db.SQLDB()}
}

// Get returns a string setting or the fallback when absent.
func (r *SettingsRepository) Get(ctx context.Context, key string, fallback string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == nil {
		return value, nil
	}
	if err == sql.ErrNoRows {
		return fallback, nil
	}
	return "", fmt.Errorf("get setting %s: %w", key, err)
}

// Set stores a string setting.
func (r *SettingsRepository) Set(ctx context.Context, key string, value string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

// GetBool returns a boolean setting or fallback when absent.
func (r *SettingsRepository) GetBool(ctx context.Context, key string, fallback bool) (bool, error) {
	value, err := r.Get(ctx, key, strconv.FormatBool(fallback))
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return fallback, nil
	}
}

// SetBool stores a boolean setting.
func (r *SettingsRepository) SetBool(ctx context.Context, key string, value bool) error {
	return r.Set(ctx, key, strconv.FormatBool(value))
}

// ConcurrencyEnabled returns the experimental concurrency setting.
func (r *SettingsRepository) ConcurrencyEnabled(ctx context.Context) (bool, error) {
	return r.GetBool(ctx, core.SettingConcurrencyEnabled, false)
}
