//go:build !linux

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func defaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}

	base := filepath.Join(home, ".patchpilot")
	return Paths{
		ConfigPath:    filepath.Join(base, "config.yaml"),
		DataDir:       base,
		DatabasePath:  filepath.Join(base, "patchpilot.db"),
		SocketPath:    filepath.Join(base, "patchpilot.sock"),
		LogPath:       filepath.Join(base, "daemon.log"),
		SecretKeyPath: filepath.Join(base, "secret.key"),
	}, nil
}
