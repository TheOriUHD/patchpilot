package systemd

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// Notify sends a state update to systemd when NOTIFY_SOCKET is present.
func Notify(state string) error {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return nil
	}
	name := socket
	if strings.HasPrefix(name, "@") {
		name = "\x00" + strings.TrimPrefix(name, "@")
	}
	addr := &net.UnixAddr{Name: name, Net: "unixgram"}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return fmt.Errorf("connect notify socket: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(state)); err != nil {
		return fmt.Errorf("send notify state: %w", err)
	}
	return nil
}

// NotifyReady tells systemd the daemon is ready to accept local IPC traffic.
func NotifyReady() error {
	return Notify("READY=1")
}
