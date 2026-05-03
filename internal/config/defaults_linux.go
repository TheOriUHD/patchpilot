//go:build linux

package config

func defaultPaths() (Paths, error) {
	return Paths{
		ConfigPath:    "/etc/patchpilot/config.yaml",
		DataDir:       "/var/lib/patchpilot",
		DatabasePath:  "/var/lib/patchpilot/patchpilot.db",
		SocketPath:    "/run/patchpilot/patchpilot.sock",
		LogPath:       "/var/log/patchpilot/daemon.log",
		SecretKeyPath: "/etc/patchpilot/secret.key",
	}, nil
}
