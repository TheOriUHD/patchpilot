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
	switch manager {
	case ManagerAPT, ManagerAPTProxmox:
		return []string{"apt-get", "apt", "dpkg", "test"}, nil
	case ManagerDNF:
		return []string{"dnf", "test"}, nil
	case ManagerPacman:
		return []string{"pacman", "test"}, nil
	case ManagerZypper:
		return []string{"zypper", "test"}, nil
	case ManagerAPK:
		return []string{"apk", "test"}, nil
	default:
		return nil, fmt.Errorf("unsupported package manager %q", manager)
	}
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
