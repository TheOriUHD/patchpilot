package core

import "time"

// HostType identifies how patchpilot manages a host.
type HostType string

const (
	// HostTypeStandard is a Linux host managed over SSH with a package manager.
	HostTypeStandard HostType = "standard"
	// HostTypeTrueNAS is a TrueNAS SCALE host managed with the TrueNAS API.
	HostTypeTrueNAS HostType = "truenas"
	// HostTypeHAOS is a Home Assistant OS host managed with the supervisor API.
	HostTypeHAOS HostType = "haos"
	// HostTypeSelf is the patchpilot machine, updated through the self-update path.
	HostTypeSelf HostType = "self"
)

// Host is the persisted inventory record for one managed system.
type Host struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Hostname        string     `json:"hostname"`
	SSHPort         int        `json:"ssh_port"`
	SSHUser         string     `json:"ssh_user"`
	Type            HostType   `json:"host_type"`
	PackageManager  string     `json:"package_manager"`
	OSPretty        string     `json:"os_pretty"`
	Arch            string     `json:"arch"`
	IsProxmoxHost   bool       `json:"is_proxmox_host"`
	IsSelf          bool       `json:"is_self"`
	Enabled         bool       `json:"enabled"`
	AutoReboot      bool       `json:"auto_reboot"`
	AutoRebootDelay int        `json:"auto_reboot_countdown_seconds"`
	Notes           string     `json:"notes"`
	LastSeenAt      *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// HostCreate describes a new host inventory record.
type HostCreate struct {
	Name               string   `json:"name"`
	Hostname           string   `json:"hostname"`
	SSHPort            int      `json:"ssh_port"`
	SSHUser            string   `json:"ssh_user"`
	Type               HostType `json:"host_type"`
	PackageManager     string   `json:"package_manager"`
	OSPretty           string   `json:"os_pretty"`
	Arch               string   `json:"arch"`
	IsProxmoxHost      bool     `json:"is_proxmox_host"`
	IsSelf             bool     `json:"is_self"`
	Enabled            bool     `json:"enabled"`
	AutoReboot         bool     `json:"auto_reboot"`
	AutoRebootDelay    int      `json:"auto_reboot_countdown_seconds"`
	Notes              string   `json:"notes"`
	APITokenCiphertext string   `json:"-"`
	APITokenNonce      string   `json:"-"`
}

// EncryptedSecret is an encrypted per-host secret stored in SQLite.
type EncryptedSecret struct {
	Ciphertext string
	Nonce      string
}

// HostUpdate describes mutable inventory fields.
type HostUpdate struct {
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	AutoReboot      bool   `json:"auto_reboot"`
	AutoRebootDelay int    `json:"auto_reboot_countdown_seconds"`
	Notes           string `json:"notes"`
}

// NormalizeRebootDelay clamps the per-host auto-reboot countdown to a sane range.
func NormalizeRebootDelay(seconds int) int {
	if seconds <= 0 {
		return 30
	}
	if seconds < 5 {
		return 5
	}
	if seconds > 3600 {
		return 3600
	}
	return seconds
}
