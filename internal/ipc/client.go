package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Client talks to the patchpilot daemon over a Unix socket.
type Client struct {
	socketPath string
	httpClient *http.Client
}

// NewClient creates an IPC client for the supplied Unix socket path.
func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 2 * time.Second}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}

	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{Transport: transport},
	}
}

// Health fetches daemon health over the local Unix socket.
func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://patchpilot/api/health", nil)
	if err != nil {
		return HealthResponse{}, fmt.Errorf("create health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return HealthResponse{}, fmt.Errorf("connect to daemon at %s: %w", c.socketPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return HealthResponse{}, fmt.Errorf("daemon health returned %s", resp.Status)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return HealthResponse{}, fmt.Errorf("decode daemon health: %w", err)
	}
	return health, nil
}

// SocketPath returns the Unix socket path used by the client.
func (c *Client) SocketPath() string {
	return c.socketPath
}
