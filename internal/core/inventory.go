package core

// InventoryBackup is the YAML backup format.
type InventoryBackup struct {
	FormatVersion      int                     `yaml:"format_version" json:"format_version"`
	ConcurrencyEnabled bool                    `yaml:"concurrency_enabled" json:"concurrency_enabled"`
	Hosts              []InventoryHost         `yaml:"hosts" json:"hosts"`
	Schedules          []InventorySchedule     `yaml:"schedules" json:"schedules"`
	ScheduleHosts      []InventoryScheduleHost `yaml:"schedule_hosts" json:"schedule_hosts"`
}

// InventoryHost is a host backup record with API tokens excluded.
type InventoryHost struct {
	ID              int64    `yaml:"id" json:"id"`
	Name            string   `yaml:"name" json:"name"`
	Hostname        string   `yaml:"hostname" json:"hostname"`
	SSHPort         int      `yaml:"ssh_port" json:"ssh_port"`
	SSHUser         string   `yaml:"ssh_user" json:"ssh_user"`
	Type            HostType `yaml:"host_type" json:"host_type"`
	PackageManager  string   `yaml:"package_manager" json:"package_manager"`
	OSPretty        string   `yaml:"os_pretty" json:"os_pretty"`
	Arch            string   `yaml:"arch" json:"arch"`
	IsProxmoxHost   bool     `yaml:"is_proxmox_host" json:"is_proxmox_host"`
	IsSelf          bool     `yaml:"is_self" json:"is_self"`
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	AutoReboot      bool     `yaml:"auto_reboot" json:"auto_reboot"`
	AutoRebootDelay int      `yaml:"auto_reboot_countdown_seconds" json:"auto_reboot_countdown_seconds"`
	Notes           string   `yaml:"notes" json:"notes"`
}

// InventorySchedule is a schedule backup record.
type InventorySchedule struct {
	ID            int64     `yaml:"id" json:"id"`
	Name          string    `yaml:"name" json:"name"`
	Frequency     Frequency `yaml:"frequency" json:"frequency"`
	Minute        int       `yaml:"minute" json:"minute"`
	Hour          int       `yaml:"hour" json:"hour"`
	DayOfWeek     int       `yaml:"day_of_week" json:"day_of_week"`
	DayOfMonth    int       `yaml:"day_of_month" json:"day_of_month"`
	Enabled       bool      `yaml:"enabled" json:"enabled"`
	MaxConcurrent int       `yaml:"max_concurrent" json:"max_concurrent"`
}

// InventoryScheduleHost links schedules to hosts.
type InventoryScheduleHost struct {
	ScheduleID int64 `yaml:"schedule_id" json:"schedule_id"`
	HostID     int64 `yaml:"host_id" json:"host_id"`
	OrderIndex int   `yaml:"order_index" json:"order_index"`
}

// InventoryConflictAction describes import conflict handling.
type InventoryConflictAction string

const (
	InventoryConflictSkip      InventoryConflictAction = "skip"
	InventoryConflictOverwrite InventoryConflictAction = "overwrite"
	InventoryConflictRename    InventoryConflictAction = "rename"
)

// InventoryImportOptions configures conflict handling by original host name.
type InventoryImportOptions struct {
	Conflicts map[string]InventoryConflictResolution `json:"conflicts"`
}

// InventoryConflictResolution contains one host conflict decision.
type InventoryConflictResolution struct {
	Action  InventoryConflictAction `json:"action"`
	NewName string                  `json:"new_name"`
}

// InventoryImportResult summarizes import effects.
type InventoryImportResult struct {
	ImportedHosts     int      `json:"imported_hosts"`
	ImportedSchedules int      `json:"imported_schedules"`
	Conflicts         []string `json:"conflicts,omitempty"`
}

// InventoryPathRequest is used by settings backup endpoints.
type InventoryPathRequest struct {
	Path      string                                 `json:"path"`
	Conflicts map[string]InventoryConflictResolution `json:"conflicts,omitempty"`
}
