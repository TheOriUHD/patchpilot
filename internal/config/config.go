package config

import (
	"context"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config contains runtime settings shared by the daemon, TUI, and CLI.
type Config struct {
	Paths Paths `yaml:"paths"`
}

// Paths contains filesystem locations used by patchpilot.
type Paths struct {
	ConfigPath   string `yaml:"-"`
	DataDir      string `yaml:"data_dir"`
	DatabasePath string `yaml:"database_path"`
	SocketPath   string `yaml:"socket_path"`
	LogPath      string `yaml:"log_path"`
}

// Load reads config.yaml when present, applies defaults, and overlays env vars.
func Load(ctx context.Context) (Config, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return Config{}, err
	}

	if value := os.Getenv("PATCHPILOT_CONFIG"); value != "" {
		paths.ConfigPath = value
	}

	cfg := Config{Paths: paths}
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}

	raw, err := os.ReadFile(cfg.Paths.ConfigPath)
	switch {
	case err == nil:
		var loaded Config
		if err := yaml.Unmarshal(raw, &loaded); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", cfg.Paths.ConfigPath, err)
		}
		cfg = mergeConfig(cfg, loaded)
	case errors.Is(err, os.ErrNotExist):
	default:
		return Config{}, fmt.Errorf("read config %s: %w", cfg.Paths.ConfigPath, err)
	}

	applyEnv(&cfg)

	if err := ctx.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// DefaultPaths returns platform-specific default filesystem locations.
func DefaultPaths() (Paths, error) {
	return defaultPaths()
}

func mergeConfig(base Config, loaded Config) Config {
	if loaded.Paths.DataDir != "" {
		base.Paths.DataDir = loaded.Paths.DataDir
	}
	if loaded.Paths.DatabasePath != "" {
		base.Paths.DatabasePath = loaded.Paths.DatabasePath
	}
	if loaded.Paths.SocketPath != "" {
		base.Paths.SocketPath = loaded.Paths.SocketPath
	}
	if loaded.Paths.LogPath != "" {
		base.Paths.LogPath = loaded.Paths.LogPath
	}
	return base
}

func applyEnv(cfg *Config) {
	if value := os.Getenv("PATCHPILOT_DATA_DIR"); value != "" {
		cfg.Paths.DataDir = value
	}
	if value := os.Getenv("PATCHPILOT_DB_PATH"); value != "" {
		cfg.Paths.DatabasePath = value
	}
	if value := os.Getenv("PATCHPILOT_SOCKET_PATH"); value != "" {
		cfg.Paths.SocketPath = value
	}
	if value := os.Getenv("PATCHPILOT_LOG_PATH"); value != "" {
		cfg.Paths.LogPath = value
	}
}
