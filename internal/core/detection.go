package core

import (
	"fmt"
	"strings"

	"github.com/TheOriUHD/patchpilot/internal/pkgmgr"
)

// StandardDetectionProbe is the single-round-trip shell probe used for Standard hosts.
const StandardDetectionProbe = `{
     echo "---OS---";       cat /etc/os-release 2>/dev/null
     echo "---KERNEL---";   uname -a
     echo "---ARCH---";     uname -m
     echo "---HOSTNAME---"; hostnamectl 2>/dev/null || hostname
     echo "---PKGMGR---"
     command -v apt-get >/dev/null && echo "apt"
     command -v dnf     >/dev/null && echo "dnf"
     command -v pacman  >/dev/null && echo "pacman"
     command -v zypper  >/dev/null && echo "zypper"
     command -v apk     >/dev/null && echo "apk"
     echo "---PROXMOX---";  test -f /etc/pve/.version && echo yes || echo no
     echo "---HAOS---";     grep -qi "Home Assistant" /etc/os-release 2>/dev/null && echo yes || echo no
     echo "---TRUENAS---";  test -f /etc/version && grep -qi truenas /etc/version && echo yes || echo no
     echo "---SUDO---";     command -v sudo >/dev/null && echo present || echo missing
     echo "---SUDO_NPW---"; sudo -n true 2>&1 >/dev/null && echo yes || echo no
     echo "---CURRENT_UID---"; id -u
     echo "---UPMGR_USER---"; id patchpilot 2>/dev/null && echo exists || echo missing
   } 2>&1`

// HostFacts is the structured result parsed from the Standard detection probe.
type HostFacts struct {
	OSPretty             string                  `json:"os_pretty"`
	OSID                 string                  `json:"os_id"`
	Kernel               string                  `json:"kernel"`
	Arch                 string                  `json:"arch"`
	Hostname             string                  `json:"hostname"`
	PackageManager       pkgmgr.PackageManager   `json:"package_manager"`
	PackageManagersFound []pkgmgr.PackageManager `json:"package_managers_found"`
	IsProxmox            bool                    `json:"is_proxmox"`
	IsHAOS               bool                    `json:"is_haos"`
	IsTrueNAS            bool                    `json:"is_truenas"`
	SudoPresent          bool                    `json:"sudo_present"`
	SudoNoPassword       bool                    `json:"sudo_no_password"`
	CurrentUID           string                  `json:"current_uid"`
	PatchpilotUserExists bool                    `json:"patchpilot_user_exists"`
	Sections             map[string]string       `json:"sections"`
}

// ParseHostFacts parses the detection probe output into HostFacts.
func ParseHostFacts(output string) (HostFacts, error) {
	sections := splitProbeSections(output)
	facts := HostFacts{Sections: sections}

	required := []string{"OS", "KERNEL", "ARCH", "HOSTNAME", "PKGMGR", "PROXMOX", "HAOS", "TRUENAS", "SUDO", "SUDO_NPW", "CURRENT_UID", "UPMGR_USER"}
	for _, section := range required {
		if _, ok := sections[section]; !ok {
			return HostFacts{}, fmt.Errorf("detection output is missing %s section", section)
		}
	}

	osRelease := parseOSRelease(sections["OS"])
	facts.OSPretty = osRelease["PRETTY_NAME"]
	if facts.OSPretty == "" {
		facts.OSPretty = strings.TrimSpace(sections["OS"])
	}
	facts.OSID = osRelease["ID"]
	facts.Kernel = firstNonEmptyLine(sections["KERNEL"])
	facts.Arch = firstNonEmptyLine(sections["ARCH"])
	facts.Hostname = parseHostnameSection(sections["HOSTNAME"])
	facts.PackageManagersFound = parsePackageManagers(sections["PKGMGR"])
	facts.PackageManager = choosePackageManager(facts.PackageManagersFound)
	facts.IsProxmox = parseYesNo(sections["PROXMOX"])
	facts.IsHAOS = parseYesNo(sections["HAOS"])
	facts.IsTrueNAS = parseYesNo(sections["TRUENAS"])
	facts.SudoPresent = strings.Contains(strings.ToLower(sections["SUDO"]), "present")
	facts.SudoNoPassword = parseYesNo(sections["SUDO_NPW"])
	facts.CurrentUID = firstNonEmptyLine(sections["CURRENT_UID"])
	facts.PatchpilotUserExists = strings.Contains(strings.ToLower(sections["UPMGR_USER"]), "exists")

	if facts.IsProxmox && facts.PackageManager == pkgmgr.ManagerAPT {
		facts.PackageManager = pkgmgr.ManagerAPTProxmox
	}
	if facts.PackageManager == "" && !facts.IsHAOS && !facts.IsTrueNAS {
		return HostFacts{}, fmt.Errorf("no supported package manager detected")
	}

	return facts, nil
}

// StandardRefusal returns a user-facing refusal message when a Standard probe finds a special host type.
func StandardRefusal(facts HostFacts) string {
	if facts.IsHAOS {
		return "This appears to be a Home Assistant OS host. Standard mode can't manage HAOS; switch to HAOS mode."
	}
	if facts.IsTrueNAS {
		return "This appears to be a TrueNAS host. Standard mode can't manage TrueNAS; switch to TrueNAS mode."
	}
	return ""
}

func splitProbeSections(output string) map[string]string {
	sections := make(map[string]string)
	current := ""
	var builder strings.Builder

	flush := func() {
		if current == "" {
			return
		}
		sections[current] = strings.TrimSpace(builder.String())
		builder.Reset()
	}

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "---") && strings.HasSuffix(trimmed, "---") && len(trimmed) > 6 {
			flush()
			current = strings.Trim(trimmed, "-")
			continue
		}
		if current == "" {
			continue
		}
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	flush()

	return sections
}

func parseOSRelease(raw string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return values
}

func parsePackageManagers(raw string) []pkgmgr.PackageManager {
	var managers []pkgmgr.PackageManager
	for _, line := range strings.Split(raw, "\n") {
		manager := pkgmgr.PackageManager(strings.TrimSpace(line))
		if pkgmgr.IsSupported(manager) {
			managers = append(managers, manager)
		}
	}
	return managers
}

func choosePackageManager(managers []pkgmgr.PackageManager) pkgmgr.PackageManager {
	if len(managers) == 0 {
		return ""
	}
	return managers[0]
}

func parseHostnameSection(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Static hostname:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Static hostname:"))
		}
	}
	return firstNonEmptyLine(raw)
}

func parseYesNo(raw string) bool {
	return firstNonEmptyLine(strings.ToLower(raw)) == "yes"
}

func firstNonEmptyLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
