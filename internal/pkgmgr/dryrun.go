package pkgmgr

import (
	"fmt"
	"regexp"
	"strings"
)

// DryRunResult summarizes the dry-run output for display and persistence.
type DryRunResult struct {
	PackagesUpgraded int    `json:"packages_upgraded"`
	PackagesRemoved  int    `json:"packages_removed"`
	DownloadSize     string `json:"download_size"`
	RebootRequired   bool   `json:"reboot_required"`
	Summary          string `json:"summary"`
	RawOutput        string `json:"raw_output"`
}

// ParseDryRunOutput extracts a package-update summary from package-manager dry-run output.
func ParseDryRunOutput(manager PackageManager, output string, exitCode int) (DryRunResult, error) {
	result := DryRunResult{RawOutput: output}
	switch manager {
	case ManagerAPT, ManagerAPTProxmox:
		parseAPT(output, &result)
	case ManagerDNF:
		if exitCode != 0 && exitCode != 100 {
			return result, fmt.Errorf("dnf check-update failed with exit code %d", exitCode)
		}
		parseDNF(output, &result)
	case ManagerPacman:
		parsePacman(output, &result)
	case ManagerZypper:
		parseZypper(output, &result)
	case ManagerAPK:
		parseAPK(output, &result)
	default:
		return result, fmt.Errorf("unsupported package manager %q", manager)
	}
	result.Summary = fmt.Sprintf("%d packages would be upgraded", result.PackagesUpgraded)
	return result, nil
}

func parseAPT(output string, result *DryRunResult) {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Inst ") || strings.Contains(trimmed, "/") && strings.Contains(trimmed, "upgradable from:") {
			result.PackagesUpgraded++
		}
		if strings.HasPrefix(trimmed, "Remv ") {
			result.PackagesRemoved++
		}
		if strings.HasPrefix(trimmed, "Need to get ") {
			result.DownloadSize = strings.TrimSuffix(strings.TrimPrefix(trimmed, "Need to get "), ".")
		}
	}
}

func parseDNF(output string, result *DryRunResult) {
	section := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Obsoleting Packages") {
			section = false
		}
		if strings.HasPrefix(trimmed, "Available Upgrades") || strings.HasPrefix(trimmed, "Upgrading:") {
			section = true
			continue
		}
		if section && looksLikeDNFPackageLine(trimmed) {
			result.PackagesUpgraded++
		}
		if strings.HasPrefix(trimmed, "Total download size:") {
			result.DownloadSize = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total download size:"))
		}
	}
}

func parsePacman(output string, result *DryRunResult) {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			result.PackagesUpgraded++
		}
		if strings.HasPrefix(trimmed, "Total Download Size:") {
			result.DownloadSize = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total Download Size:"))
		}
	}
}

func parseZypper(output string, result *DryRunResult) {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "v |") || strings.HasPrefix(trimmed, "v|") {
			result.PackagesUpgraded++
		}
	}
}

func parseAPK(output string, result *DryRunResult) {
	re := regexp.MustCompile(`(?m)^\s*([a-zA-Z0-9_.+:-]+)\s+[0-9][^\s]*\s+->\s+[0-9]`)
	result.PackagesUpgraded = len(re.FindAllStringSubmatch(output, -1))
}

func looksLikeDNFPackageLine(line string) bool {
	if line == "" || strings.HasPrefix(line, "Last metadata") || strings.HasPrefix(line, "Name ") || strings.HasPrefix(line, "=") {
		return false
	}
	fields := strings.Fields(line)
	return len(fields) >= 3 && strings.Contains(fields[1], ".")
}
