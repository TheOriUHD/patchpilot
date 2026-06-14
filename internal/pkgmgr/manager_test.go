package pkgmgr

import (
	"strings"
	"testing"
)

func TestSudoersEntry(t *testing.T) {
	t.Parallel()

	paths := map[string]string{"apt-get": "/usr/local/bin/apt-get", "apt": "/bin/apt", "dpkg": "/usr/bin/dpkg", "test": "/bin/test"}
	entry, err := SudoersEntry("patchpilot", ManagerAPT, paths)
	if err != nil {
		t.Fatalf("SudoersEntry() error: %v", err)
	}
	for _, want := range []string{"patchpilot ALL=(ALL) NOPASSWD:", "/usr/local/bin/apt-get", "/bin/apt", "/usr/bin/dpkg", "/bin/test"} {
		if !strings.Contains(entry, want) {
			t.Fatalf("entry %q missing %q", entry, want)
		}
	}

	if _, err := SudoersEntry("patchpilot", ManagerAPT, map[string]string{"apt-get": "apt-get"}); err == nil {
		t.Fatal("SudoersEntry() expected missing absolute path error")
	}
}

func TestCommandGeneration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		manager PackageManager
		paths   map[string]string
		want    string
	}{
		{ManagerAPT, map[string]string{"apt-get": "/a/apt-get", "apt": "/a/apt"}, "dist-upgrade"},
		{ManagerDNF, map[string]string{"dnf": "/d/dnf"}, "check-update"},
		{ManagerPacman, map[string]string{"pacman": "/p/pacman"}, "-Sup"},
		{ManagerZypper, map[string]string{"zypper": "/z/zypper"}, "list-updates"},
		{ManagerAPK, map[string]string{"apk": "/sbin/apk"}, "-s upgrade"},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.manager), func(t *testing.T) {
			t.Parallel()

			command, err := DryRunCommand(test.manager, test.paths)
			if err != nil {
				t.Fatalf("DryRunCommand() error: %v", err)
			}
			if !strings.Contains(command, test.want) {
				t.Fatalf("DryRunCommand() = %q, want substring %q", command, test.want)
			}
		})
	}
}

func TestRebootRequiredCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		manager PackageManager
		paths   map[string]string
		wants   []string
	}{
		{"apt", ManagerAPT, map[string]string{"test": "/usr/bin/test"}, []string{"/var/run/reboot-required", "needrestart"}},
		{"dnf", ManagerDNF, map[string]string{"dnf": "/usr/bin/dnf", "needs-restarting": "/usr/bin/needs-restarting"}, []string{"/usr/bin/needs-restarting -r", "test \"$code\" -eq 1"}},
		{"pacman", ManagerPacman, map[string]string{"pacman": "/usr/bin/pacman"}, []string{"uname -r", "pacman", "-Q linux"}},
		{"apk", ManagerAPK, map[string]string{"apk": "/sbin/apk"}, []string{"/var/run/reboot-required", "uname -r", "info -v 'linux-*'"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			command, err := RebootRequiredCommand(test.manager, test.paths)
			if err != nil {
				t.Fatalf("RebootRequiredCommand() error: %v", err)
			}
			for _, want := range test.wants {
				if !strings.Contains(command, want) {
					t.Fatalf("RebootRequiredCommand() = %q, want substring %q", command, want)
				}
			}
		})
	}
}

func TestParseDryRunOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		manager PackageManager
		output  string
		exit    int
		want    int
	}{
		{"apt", ManagerAPT, "Inst openssl [1] (2 Debian)\nRemv oldpkg\nNeed to get 47 MB of archives.\nlibc/bookworm 1 amd64 [upgradable from: 0]", 0, 2},
		{"dnf", ManagerDNF, "Available Upgrades\nvim.x86_64 9.0 fedora\nkernel.x86_64 6.9 fedora\nTotal download size: 120 M", 100, 2},
		{"pacman", ManagerPacman, "https://mirror/pkg-1.pkg.tar.zst\nhttps://mirror/pkg-2.pkg.tar.zst", 0, 2},
		{"zypper", ManagerZypper, "v | repo | bash | 5.2 | x86_64\nv | repo | vim | 9 | x86_64", 0, 2},
		{"apk", ManagerAPK, "openssl 3.0 -> 3.1\nbusybox 1.36 -> 1.37", 0, 2},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseDryRunOutput(test.manager, test.output, test.exit)
			if err != nil {
				t.Fatalf("ParseDryRunOutput() error: %v", err)
			}
			if got.PackagesUpgraded != test.want {
				t.Fatalf("PackagesUpgraded = %d, want %d", got.PackagesUpgraded, test.want)
			}
		})
	}
}
