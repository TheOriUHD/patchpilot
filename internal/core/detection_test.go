package core

import (
	"strings"
	"testing"

	"github.com/TheOriUHD/patchpilot/internal/pkgmgr"
)

func TestParseHostFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		output  string
		want    HostFacts
		wantErr string
	}{
		{
			name:   "debian apt",
			output: probeOutput("PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian", "Linux host 6.1", "x86_64", "serverph", "apt", "no", "no", "no", "present", "no", "present", "present", "present", "present", "1000", "missing"),
			want: HostFacts{
				OSPretty:              "Debian GNU/Linux 12 (bookworm)",
				OSID:                  "debian",
				Kernel:                "Linux host 6.1",
				Arch:                  "x86_64",
				Hostname:              "serverph",
				PackageManager:        pkgmgr.ManagerAPT,
				PackageManagersFound:  []pkgmgr.PackageManager{pkgmgr.ManagerAPT},
				SudoPresent:           true,
				SudoersFilePresent:    true,
				SudoersDirPresent:     true,
				SudoersIncludePresent: true,
				SuPresent:             true,
				CurrentUID:            "1000",
			},
		},
		{
			name:   "proxmox uses apt_proxmox",
			output: probeOutput("PRETTY_NAME=\"Debian GNU/Linux 12\"", "Linux pve", "x86_64", "pve", "apt", "yes", "no", "no", "present", "yes", "present", "present", "present", "present", "0", "exists"),
			want:   HostFacts{PackageManager: pkgmgr.ManagerAPTProxmox, IsProxmox: true, SudoPresent: true, SudoNoPassword: true, SudoersFilePresent: true, SudoersDirPresent: true, SudoersIncludePresent: true, SuPresent: true, PatchpilotUserExists: true, CurrentUID: "0"},
		},
		{
			name:   "minimal debian without sudo has su",
			output: probeOutput("PRETTY_NAME=\"Debian GNU/Linux 12\"", "Linux db", "x86_64", "mc-database", "apt", "no", "no", "no", "missing", "no", "missing", "missing", "missing", "present", "1000", "missing"),
			want:   HostFacts{PackageManager: pkgmgr.ManagerAPT, SudoPresent: false, SuPresent: true, CurrentUID: "1000"},
		},
		{
			name:    "missing section",
			output:  "---OS---\nPRETTY_NAME=broken",
			wantErr: "missing KERNEL section",
		},
		{
			name:    "no package manager",
			output:  probeOutput("PRETTY_NAME=\"Mystery\"", "Linux", "x86_64", "mystery", "", "no", "no", "no", "present", "no", "present", "present", "present", "missing", "1000", "missing"),
			wantErr: "no supported package manager detected",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseHostFacts(test.output)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("ParseHostFacts() error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHostFacts() unexpected error: %v", err)
			}
			if test.want.PackageManager != "" && got.PackageManager != test.want.PackageManager {
				t.Fatalf("PackageManager = %q, want %q", got.PackageManager, test.want.PackageManager)
			}
			if test.want.OSPretty != "" && got.OSPretty != test.want.OSPretty {
				t.Fatalf("OSPretty = %q, want %q", got.OSPretty, test.want.OSPretty)
			}
			if got.IsProxmox != test.want.IsProxmox {
				t.Fatalf("IsProxmox = %v, want %v", got.IsProxmox, test.want.IsProxmox)
			}
			if got.SuPresent != test.want.SuPresent {
				t.Fatalf("SuPresent = %v, want %v", got.SuPresent, test.want.SuPresent)
			}
			if got.SudoersFilePresent != test.want.SudoersFilePresent {
				t.Fatalf("SudoersFilePresent = %v, want %v", got.SudoersFilePresent, test.want.SudoersFilePresent)
			}
			if got.SudoersDirPresent != test.want.SudoersDirPresent {
				t.Fatalf("SudoersDirPresent = %v, want %v", got.SudoersDirPresent, test.want.SudoersDirPresent)
			}
			if got.SudoersIncludePresent != test.want.SudoersIncludePresent {
				t.Fatalf("SudoersIncludePresent = %v, want %v", got.SudoersIncludePresent, test.want.SudoersIncludePresent)
			}
		})
	}
}

func TestStandardRefusal(t *testing.T) {
	t.Parallel()

	if got := StandardRefusal(HostFacts{IsHAOS: true}); !strings.Contains(got, "Home Assistant OS") {
		t.Fatalf("HAOS refusal = %q", got)
	}
	if got := StandardRefusal(HostFacts{IsTrueNAS: true}); !strings.Contains(got, "TrueNAS") {
		t.Fatalf("TrueNAS refusal = %q", got)
	}
}

func probeOutput(osRelease string, kernel string, arch string, hostname string, pkg string, proxmox string, haos string, truenas string, sudo string, sudoNPW string, sudoersFile string, sudoersDir string, sudoersInclude string, su string, uid string, upmgr string) string {
	return strings.Join([]string{
		"---OS---", osRelease,
		"---KERNEL---", kernel,
		"---ARCH---", arch,
		"---HOSTNAME---", hostname,
		"---PKGMGR---", pkg,
		"---PROXMOX---", proxmox,
		"---HAOS---", haos,
		"---TRUENAS---", truenas,
		"---SUDO---", sudo,
		"---SUDO_NPW---", sudoNPW,
		"---SUDOERS_FILE---", sudoersFile,
		"---SUDOERS_DIR---", sudoersDir,
		"---SUDOERS_INCLUDE---", sudoersInclude,
		"---SU---", su,
		"---CURRENT_UID---", uid,
		"---UPMGR_USER---", upmgr,
	}, "\n")
}
