package ipc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/pkgmgr"
	"github.com/TheOriUHD/patchpilot/internal/stream"
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
	var health HealthResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/health", nil, &health); err != nil {
		return HealthResponse{}, err
	}
	return health, nil
}

// ListHosts fetches all inventory hosts.
func (c *Client) ListHosts(ctx context.Context) ([]core.Host, error) {
	var hosts []core.Host
	if err := c.doJSON(ctx, http.MethodGet, "/api/hosts/", nil, &hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

// GetHost fetches a single inventory host.
func (c *Client) GetHost(ctx context.Context, id int64) (core.Host, error) {
	var host core.Host
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/hosts/%d", id), nil, &host); err != nil {
		return core.Host{}, err
	}
	return host, nil
}

// UpdateHost updates mutable host fields.
func (c *Client) UpdateHost(ctx context.Context, id int64, input core.HostUpdate) (core.Host, error) {
	var host core.Host
	if err := c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/api/hosts/%d", id), input, &host); err != nil {
		return core.Host{}, err
	}
	return host, nil
}

// ToggleHost toggles host enabled state.
func (c *Client) ToggleHost(ctx context.Context, id int64) (core.Host, error) {
	var host core.Host
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/hosts/%d/toggle", id), nil, &host); err != nil {
		return core.Host{}, err
	}
	return host, nil
}

// DeleteHost removes a host from inventory.
func (c *Client) DeleteHost(ctx context.Context, id int64) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("/api/hosts/%d", id), nil, nil)
}

// DryRunHost runs a dry-run for a host.
func (c *Client) DryRunHost(ctx context.Context, id int64) (pkgmgr.DryRunResult, error) {
	var result pkgmgr.DryRunResult
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/hosts/%d/dry-run", id), nil, &result); err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	return result, nil
}

// RunHostNow starts a manual single-host run.
func (c *Client) RunHostNow(ctx context.Context, id int64) (int64, error) {
	detail, err := c.RunHostNowDetail(ctx, id)
	if err != nil {
		return 0, err
	}
	return detail.Run.ID, nil
}

// RunHostNowDetail starts a manual single-host run and returns its persisted detail.
func (c *Client) RunHostNowDetail(ctx context.Context, id int64) (core.RunDetail, error) {
	var detail core.RunDetail
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/hosts/%d/run", id), nil, &detail); err != nil {
		return core.RunDetail{}, err
	}
	return detail, nil
}

// ListSchedules fetches all schedules.
func (c *Client) ListSchedules(ctx context.Context) ([]core.Schedule, error) {
	var schedules []core.Schedule
	if err := c.doJSON(ctx, http.MethodGet, "/api/schedules/", nil, &schedules); err != nil {
		return nil, err
	}
	return schedules, nil
}

// CreateSchedule creates a schedule.
func (c *Client) CreateSchedule(ctx context.Context, input core.ScheduleInput) (core.Schedule, error) {
	var schedule core.Schedule
	if err := c.doJSON(ctx, http.MethodPost, "/api/schedules/", input, &schedule); err != nil {
		return core.Schedule{}, err
	}
	return schedule, nil
}

// UpdateSchedule updates a schedule.
func (c *Client) UpdateSchedule(ctx context.Context, id int64, input core.ScheduleInput) (core.Schedule, error) {
	var schedule core.Schedule
	if err := c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/api/schedules/%d", id), input, &schedule); err != nil {
		return core.Schedule{}, err
	}
	return schedule, nil
}

// DeleteSchedule removes a schedule.
func (c *Client) DeleteSchedule(ctx context.Context, id int64) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("/api/schedules/%d", id), nil, nil)
}

// RunScheduleNow starts or queues a manual run for a schedule.
func (c *Client) RunScheduleNow(ctx context.Context, id int64) (core.RunDetail, error) {
	var detail core.RunDetail
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/schedules/%d/run", id), nil, &detail); err != nil {
		return core.RunDetail{}, err
	}
	return detail, nil
}

// ListRuns fetches recent runs.
func (c *Client) ListRuns(ctx context.Context) ([]core.Run, error) {
	return c.ListRunsPage(ctx, core.RunListQuery{Limit: 100})
}

// ListRunsPage fetches paginated run history.
func (c *Client) ListRunsPage(ctx context.Context, query core.RunListQuery) ([]core.Run, error) {
	var runs []core.Run
	path := fmt.Sprintf("/api/runs/?limit=%d&offset=%d", query.Limit, query.Offset)
	if len(query.Statuses) > 0 {
		parts := make([]string, 0, len(query.Statuses))
		for _, status := range query.Statuses {
			parts = append(parts, string(status))
		}
		path += "&status=" + strings.Join(parts, ",")
	}
	if len(query.Triggers) > 0 {
		parts := make([]string, 0, len(query.Triggers))
		for _, trigger := range query.Triggers {
			parts = append(parts, string(trigger))
		}
		path += "&trigger=" + strings.Join(parts, ",")
	}
	if query.Since != nil {
		duration := time.Since(*query.Since)
		switch {
		case duration <= 25*time.Hour:
			path += "&range=24h"
		case duration <= 8*24*time.Hour:
			path += "&range=7d"
		case duration <= 31*24*time.Hour:
			path += "&range=30d"
		}
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

// GetRun fetches one run detail.
func (c *Client) GetRun(ctx context.Context, id int64) (core.RunDetail, error) {
	var detail core.RunDetail
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/runs/%d", id), nil, &detail); err != nil {
		return core.RunDetail{}, err
	}
	return detail, nil
}

// GetRunLogs fetches persisted log lines for a historical run.
func (c *Client) GetRunLogs(ctx context.Context, id int64) (core.RunLogResponse, error) {
	var logs core.RunLogResponse
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/runs/%d/logs", id), nil, &logs); err != nil {
		return core.RunLogResponse{}, err
	}
	return logs, nil
}

// Settings fetches the daemon settings snapshot.
func (c *Client) Settings(ctx context.Context) (core.SettingsSnapshot, error) {
	var snapshot core.SettingsSnapshot
	if err := c.doJSON(ctx, http.MethodGet, "/api/settings/", nil, &snapshot); err != nil {
		return core.SettingsSnapshot{}, err
	}
	return snapshot, nil
}

// SetConcurrency toggles experimental concurrency.
func (c *Client) SetConcurrency(ctx context.Context, enabled bool) (core.SettingsSnapshot, error) {
	var snapshot core.SettingsSnapshot
	if err := c.doJSON(ctx, http.MethodPut, "/api/settings/concurrency", core.SettingsUpdate{ConcurrencyEnabled: enabled}, &snapshot); err != nil {
		return core.SettingsSnapshot{}, err
	}
	return snapshot, nil
}

// RotateKeypair starts SSH key rotation.
func (c *Client) RotateKeypair(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodPost, "/api/settings/ssh/rotate", nil, nil)
}

// ExportInventory writes a daemon-side inventory backup.
func (c *Client) ExportInventory(ctx context.Context, path string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/settings/inventory/export", core.InventoryPathRequest{Path: path}, nil)
}

// ImportInventory imports a daemon-side inventory backup.
func (c *Client) ImportInventory(ctx context.Context, request core.InventoryPathRequest) (core.InventoryImportResult, error) {
	var result core.InventoryImportResult
	if err := c.doJSON(ctx, http.MethodPost, "/api/settings/inventory/import", request, &result); err != nil {
		return core.InventoryImportResult{}, err
	}
	return result, nil
}

// CancelRun cancels a run.
func (c *Client) CancelRun(ctx context.Context, id int64) error {
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/runs/%d/cancel", id), nil, nil)
}

// DetectStandard runs the Standard host detection flow.
func (c *Client) DetectStandard(ctx context.Context, input core.StandardDetectInput) (core.StandardDetectResult, error) {
	var result core.StandardDetectResult
	body := map[string]any{"hostname": input.Hostname, "ssh_port": input.SSHPort, "ssh_user": input.SSHUser, "password": string(input.Password)}
	if err := c.doJSON(ctx, http.MethodPost, "/api/wizard/standard/detect", body, &result); err != nil {
		return core.StandardDetectResult{}, err
	}
	return result, nil
}

// RootAssist runs a root-assisted sudo fix.
func (c *Client) RootAssist(ctx context.Context, input core.RootAssistInput) error {
	body := map[string]any{"hostname": input.Hostname, "ssh_port": input.SSHPort, "ssh_user": input.SSHUser, "root_password": string(input.RootPassword), "facts": input.Facts, "diagnosis": input.Diagnosis}
	return c.doJSON(ctx, http.MethodPost, "/api/wizard/standard/root-assist", body, nil)
}

// StartBootstrap starts Standard bootstrap and returns a stream id.
func (c *Client) StartBootstrap(ctx context.Context, input core.BootstrapStartInput) (core.BootstrapSession, error) {
	var session core.BootstrapSession
	body := map[string]any{"hostname": input.Hostname, "ssh_port": input.SSHPort, "ssh_user": input.SSHUser, "password": string(input.Password), "facts": input.Facts}
	if err := c.doJSON(ctx, http.MethodPost, "/api/wizard/standard/bootstrap", body, &session); err != nil {
		return core.BootstrapSession{}, err
	}
	return session, nil
}

// VerifyAPIHost verifies a native API host endpoint and token.
func (c *Client) VerifyAPIHost(ctx context.Context, input core.APIHostVerifyInput) (core.APIHostVerifyResult, error) {
	var result core.APIHostVerifyResult
	body := map[string]any{"host_type": input.Type, "endpoint": input.Endpoint, "api_token": string(input.APIToken)}
	if err := c.doJSON(ctx, http.MethodPost, "/api/wizard/api/verify", body, &result); err != nil {
		return core.APIHostVerifyResult{}, err
	}
	return result, nil
}

// CheckAPIHost performs the native API host update-check dry-run.
func (c *Client) CheckAPIHost(ctx context.Context, input core.APIHostVerifyInput) (core.APIHostVerifyResult, error) {
	var result core.APIHostVerifyResult
	body := map[string]any{"host_type": input.Type, "endpoint": input.Endpoint, "api_token": string(input.APIToken)}
	if err := c.doJSON(ctx, http.MethodPost, "/api/wizard/api/check", body, &result); err != nil {
		return core.APIHostVerifyResult{}, err
	}
	return result, nil
}

// CommitAPIHost saves a verified native API host to inventory.
func (c *Client) CommitAPIHost(ctx context.Context, input core.APIHostCommitInput) (core.Host, error) {
	var host core.Host
	body := map[string]any{"host_type": input.Type, "endpoint": input.Endpoint, "api_token": string(input.APIToken), "name": input.Name, "notes": input.Notes, "enabled": input.Enabled}
	if err := c.doJSON(ctx, http.MethodPost, "/api/wizard/api/commit", body, &host); err != nil {
		return core.Host{}, err
	}
	return host, nil
}

// CommitBootstrap saves a completed bootstrap session to inventory.
func (c *Client) CommitBootstrap(ctx context.Context, input core.CommitHostInput) (core.Host, error) {
	var host core.Host
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/wizard/%s/commit", input.SessionID), input, &host); err != nil {
		return core.Host{}, err
	}
	return host, nil
}

// CancelBootstrap cancels an in-flight bootstrap wizard session.
func (c *Client) CancelBootstrap(ctx context.Context, sessionID string) error {
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/wizard/%s/cancel", sessionID), nil, nil)
}

// WizardEvents subscribes to a bootstrap wizard SSE stream.
func (c *Client) WizardEvents(ctx context.Context, sessionID string) (<-chan stream.Event, <-chan error) {
	return c.events(ctx, "/api/wizard/"+sessionID+"/stream")
}

// RunEvents subscribes to run status and log events.
func (c *Client) RunEvents(ctx context.Context, runID int64) (<-chan stream.Event, <-chan error) {
	return c.events(ctx, fmt.Sprintf("/api/runs/%d/stream", runID))
}

func (c *Client) events(ctx context.Context, path string) (<-chan stream.Event, <-chan error) {
	events := make(chan stream.Event, 16)
	errors := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errors)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://patchpilot"+path, nil)
		if err != nil {
			errors <- fmt.Errorf("create stream request: %w", err)
			return
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			errors <- fmt.Errorf("connect to stream: %w", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errors <- fmt.Errorf("stream returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
			return
		}
		if err := parseSSE(ctx, resp.Body, events); err != nil {
			errors <- err
		}
	}()
	return events, errors
}

// SocketPath returns the Unix socket path used by the client.
func (c *Client) SocketPath() string {
	return c.socketPath
}

func (c *Client) doJSON(ctx context.Context, method string, path string, input any, output any) error {
	var body io.Reader
	if input != nil {
		buffer := new(bytes.Buffer)
		if err := json.NewEncoder(buffer).Encode(input); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = buffer
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://patchpilot"+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to daemon at %s: %w", c.socketPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		var errorBody struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errorBody); err == nil && errorBody.Error != "" {
			return fmt.Errorf("daemon returned %s: %s", resp.Status, errorBody.Error)
		}
		return fmt.Errorf("daemon returned %s", resp.Status)
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func parseSSE(ctx context.Context, reader io.Reader, events chan<- stream.Event) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var current stream.Event
	var data strings.Builder
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" {
			if current.Type != "" || data.Len() > 0 {
				current.Data = json.RawMessage(data.String())
				events <- current
				current = stream.Event{}
				data.Reset()
			}
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			id, _ := strconv.ParseInt(value, 10, 64)
			current.ID = id
		case "event":
			current.Type = value
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read event stream: %w", err)
	}
	return nil
}
