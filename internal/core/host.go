package core

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
	ID             int64
	Name           string
	Hostname       string
	SSHPort        int
	SSHUser        string
	Type           HostType
	PackageManager string
	OSPretty       string
	Arch           string
	IsProxmoxHost  bool
	IsSelf         bool
	Enabled        bool
	Notes          string
}
