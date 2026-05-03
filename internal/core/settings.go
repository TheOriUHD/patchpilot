package core

import "time"

const (
	// SettingConcurrencyEnabled gates experimental host concurrency.
	SettingConcurrencyEnabled = "experimental_concurrency_enabled"
)

// SettingsSnapshot contains daemon and operator settings shown by the TUI.
type SettingsSnapshot struct {
	Paths              SettingsPaths   `json:"paths"`
	ConcurrencyEnabled bool            `json:"concurrency_enabled"`
	PublicKey          string          `json:"public_key"`
	SelfUpdateLogs     []SelfUpdateLog `json:"self_update_logs"`
	DaemonLogTail      []string        `json:"daemon_log_tail"`
	About              AboutInfo       `json:"about"`
}

// SettingsPaths contains read-only daemon paths.
type SettingsPaths struct {
	DataDir       string `json:"data_dir"`
	DatabasePath  string `json:"database_path"`
	SocketPath    string `json:"socket_path"`
	LogPath       string `json:"log_path"`
	SecretKeyPath string `json:"secret_key_path"`
}

// AboutInfo describes the running daemon binary and platform.
type AboutInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	OSArch    string `json:"os_arch"`
}

// SelfUpdateLog describes a completed or attempted self-update log file.
type SelfUpdateLog struct {
	RunID      int64     `json:"run_id"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// SettingsUpdate describes mutable daemon settings.
type SettingsUpdate struct {
	ConcurrencyEnabled bool `json:"concurrency_enabled"`
}
