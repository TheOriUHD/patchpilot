package pkgmgr

import "fmt"

// PackageManager identifies a supported update mechanism.
type PackageManager string

const (
	// ManagerAPT uses apt-get dist-upgrade for Debian and Ubuntu hosts.
	ManagerAPT PackageManager = "apt"
	// ManagerAPTProxmox uses apt-get dist-upgrade for Proxmox VE hosts.
	ManagerAPTProxmox PackageManager = "apt_proxmox"
	// ManagerDNF uses dnf for Fedora and RHEL-family hosts.
	ManagerDNF PackageManager = "dnf"
	// ManagerPacman uses pacman for Arch hosts.
	ManagerPacman PackageManager = "pacman"
	// ManagerZypper uses zypper for openSUSE hosts.
	ManagerZypper PackageManager = "zypper"
	// ManagerAPK uses apk for Alpine hosts.
	ManagerAPK PackageManager = "apk"
	// ManagerTrueNASAPI uses the TrueNAS SCALE API.
	ManagerTrueNASAPI PackageManager = "truenas_api"
	// ManagerHAOSAPI uses the Home Assistant OS supervisor API.
	ManagerHAOSAPI PackageManager = "haos_api"
)

// IsSupported reports whether the manager is known to patchpilot.
func IsSupported(manager PackageManager) bool {
	switch manager {
	case ManagerAPT, ManagerAPTProxmox, ManagerDNF, ManagerPacman, ManagerZypper, ManagerAPK, ManagerTrueNASAPI, ManagerHAOSAPI:
		return true
	default:
		return false
	}
}

// RequiredCommands returns executable names that must be resolved on the target host.
func RequiredCommands(manager PackageManager) ([]string, error) {
	rebootCommands := RebootCommands()
	switch manager {
	case ManagerAPT, ManagerAPTProxmox:
		return append([]string{"apt-get", "apt", "dpkg", "test"}, rebootCommands...), nil
	case ManagerDNF:
		return append([]string{"dnf", "needs-restarting", "test"}, rebootCommands...), nil
	case ManagerPacman:
		return append([]string{"pacman", "test"}, rebootCommands...), nil
	case ManagerZypper:
		return append([]string{"zypper", "test"}, rebootCommands...), nil
	case ManagerAPK:
		return append([]string{"apk", "test"}, rebootCommands...), nil
	default:
		return nil, fmt.Errorf("unsupported package manager %q", manager)
	}
}

// RebootCommands returns reboot-related executable names required for post-update reboot actions.
func RebootCommands() []string {
	return []string{"systemctl", "reboot", "shutdown"}
}

// RebootCommand returns the daemon-issued reboot command, preferring systemd and falling back.
func RebootCommand(paths map[string]string) (string, error) {
	for _, name := range RebootCommands() {
		if paths[name] == "" {
			return "", fmt.Errorf("missing resolved absolute path for %s", name)
		}
	}
	return fmt.Sprintf("sudo -n %s reboot || sudo -n %s || sudo -n %s -r now", paths["systemctl"], paths["reboot"], paths["shutdown"]), nil
}

// DryRunCommand returns the update dry-run command for the package manager.
func DryRunCommand(manager PackageManager, paths map[string]string) (string, error) {
	switch manager {
	case ManagerAPT, ManagerAPTProxmox:
		return fmt.Sprintf("sudo -n %s update && sudo -n %s -s dist-upgrade && sudo -n %s list --upgradable", paths["apt-get"], paths["apt-get"], paths["apt"]), nil
	case ManagerDNF:
		return fmt.Sprintf("sudo -n %s check-update", paths["dnf"]), nil
	case ManagerPacman:
		return fmt.Sprintf("sudo -n %s -Sup", paths["pacman"]), nil
	case ManagerZypper:
		return fmt.Sprintf("sudo -n %s -n list-updates", paths["zypper"]), nil
	case ManagerAPK:
		return fmt.Sprintf("sudo -n %s -s upgrade", paths["apk"]), nil
	default:
		return "", fmt.Errorf("unsupported package manager %q", manager)
	}
}

// UpdateCommand returns the real update command for a single-host manual run.
func UpdateCommand(manager PackageManager, paths map[string]string) (string, error) {
	switch manager {
	case ManagerAPT, ManagerAPTProxmox:
		return fmt.Sprintf("sudo -n %s update && DEBIAN_FRONTEND=noninteractive sudo -n %s -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold dist-upgrade && sudo -n %s -y autoremove", paths["apt-get"], paths["apt-get"], paths["apt-get"]), nil
	case ManagerDNF:
		return fmt.Sprintf("sudo -n %s -y upgrade", paths["dnf"]), nil
	case ManagerPacman:
		return fmt.Sprintf("sudo -n %s -Syu --noconfirm", paths["pacman"]), nil
	case ManagerZypper:
		return fmt.Sprintf("sudo -n %s -n update", paths["zypper"]), nil
	case ManagerAPK:
		return fmt.Sprintf("sudo -n %s update && sudo -n %s upgrade", paths["apk"], paths["apk"]), nil
	default:
		return "", fmt.Errorf("unsupported package manager %q", manager)
	}
}

// VerifyCommand returns a harmless sudo command used to confirm NOPASSWD works.
func VerifyCommand(manager PackageManager, paths map[string]string) (string, error) {
	switch manager {
	case ManagerAPT, ManagerAPTProxmox:
		return fmt.Sprintf("sudo -n %s --version", paths["apt-get"]), nil
	case ManagerDNF:
		return fmt.Sprintf("sudo -n %s --version", paths["dnf"]), nil
	case ManagerPacman:
		return fmt.Sprintf("sudo -n %s --version", paths["pacman"]), nil
	case ManagerZypper:
		return fmt.Sprintf("sudo -n %s --version", paths["zypper"]), nil
	case ManagerAPK:
		return fmt.Sprintf("sudo -n %s --version", paths["apk"]), nil
	default:
		return "", fmt.Errorf("unsupported package manager %q", manager)
	}
}

// RebootRequiredCommand returns a command that exits 0 when a reboot is required and non-zero otherwise.
func RebootRequiredCommand(manager PackageManager, paths map[string]string) (string, error) {
	switch manager {
	case ManagerAPT, ManagerAPTProxmox:
		return fmt.Sprintf("sudo -n %s -f /var/run/reboot-required && exit 0; if command -v needrestart >/dev/null 2>&1; then needrestart -b -r l 2>/dev/null | grep -Eq 'NEEDRESTART-KSTA: [^1]'; else exit 1; fi", paths["test"]), nil
	case ManagerDNF:
		return fmt.Sprintf("sudo -n %s -r >/dev/null 2>&1; code=$?; test \"$code\" -eq 1", paths["needs-restarting"]), nil
	case ManagerPacman:
		return fmt.Sprintf("running=$(uname -r); installed=$(sudo -n %s -Q linux 2>/dev/null | awk '{print $2}'); test -n \"$installed\" && case \"$running\" in $installed*) exit 1;; *) exit 0;; esac", paths["pacman"]), nil
	case ManagerZypper:
		return fmt.Sprintf("running=$(uname -r); installed=$(sudo -n %s -n search -si kernel-default 2>/dev/null | awk '/^Version/ {print $3; exit}'); test -n \"$installed\" && case \"$running\" in $installed*) exit 1;; *) exit 0;; esac", paths["zypper"]), nil
	case ManagerAPK:
		return fmt.Sprintf("test -f /var/run/reboot-required && exit 0; running=$(uname -r); installed=$(sudo -n %s info -v 'linux-*' 2>/dev/null | head -n 1 | sed 's/^[^-]*-//'); test -n \"$installed\" && case \"$running\" in $installed*) exit 1;; *) exit 0;; esac", paths["apk"]), nil
	default:
		return "", fmt.Errorf("unsupported package manager %q", manager)
	}
}
