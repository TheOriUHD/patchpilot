package pkgmgr

import (
	"fmt"
	"sort"
	"strings"
)

// SudoersEntry builds the least-privilege sudoers entry for patchpilot with resolved paths.
func SudoersEntry(user string, manager PackageManager, resolvedPaths map[string]string) (string, error) {
	required, err := RequiredCommands(manager)
	if err != nil {
		return "", err
	}

	allowed := make([]string, 0, len(required))
	for _, name := range required {
		path := strings.TrimSpace(resolvedPaths[name])
		if path == "" || !strings.HasPrefix(path, "/") {
			return "", fmt.Errorf("missing resolved absolute path for %s", name)
		}
		allowed = append(allowed, sudoersCommandSpec(name, path))
	}
	sort.Strings(allowed)

	return fmt.Sprintf("%s ALL=(ALL) NOPASSWD: %s\n", user, strings.Join(allowed, ", ")), nil
}

func sudoersCommandSpec(name string, path string) string {
	switch name {
	case "systemctl":
		return path + " reboot"
	case "shutdown":
		return path + " -r now"
	default:
		return path
	}
}
