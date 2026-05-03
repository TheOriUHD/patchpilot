package selfupdate

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildSystemdRun(t *testing.T) {
	name, args := BuildSystemdRun(42, "/var/log/patchpilot/self-update-42.log")
	if name != "systemd-run" {
		t.Fatalf("name = %q", name)
	}
	wantPrefix := []string{"--unit", "patchpilot-self-update-42.service", "--on-active=30s", "/bin/sh", "-c"}
	if len(args) != 6 || !reflect.DeepEqual(args[:5], wantPrefix) {
		t.Fatalf("args = %#v", args)
	}
	command := args[5]
	for _, part := range []string{"apt-get update", "DEBIAN_FRONTEND=noninteractive", "Dpkg::Options::=\"--force-confdef\"", "Dpkg::Options::=\"--force-confold\"", "dist-upgrade", "apt-get -y autoremove", "> '/var/log/patchpilot/self-update-42.log' 2>&1"} {
		if !strings.Contains(command, part) {
			t.Fatalf("command %q missing %q", command, part)
		}
	}
}
