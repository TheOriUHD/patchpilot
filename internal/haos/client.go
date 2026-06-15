package haos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const errorBodyLimit = 200

var (
	// ErrAuth is returned when Home Assistant rejects the supplied LLAT.
	ErrAuth = errors.New("auth failed")
	// ErrAPI is returned when the server does not behave like the HAOS API.
	ErrAPI = errors.New("API call failed")
)

// Client talks to Home Assistant Core's /api/hassio Supervisor proxy.
type Client struct {
	BaseURL      string
	Token        string
	HTTPClient   *http.Client
	PollInterval time.Duration
}

// HTTPError describes a non-2xx response from Home Assistant.
type HTTPError struct {
	Cause      error
	Method     string
	Path       string
	Status     string
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	message := strings.TrimSpace(fmt.Sprintf("%s at %s", e.Status, e.Path))
	if e.Body != "" {
		message += ": " + e.Body
	}
	return message
}

func (e *HTTPError) Unwrap() error {
	return e.Cause
}

// Info contains HAOS component versions.
type Info struct {
	Supervisor Component `json:"supervisor"`
	Core       Component `json:"core"`
}

// Component contains one updateable HAOS component's version state.
type Component struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

// UpdatePlan contains the deterministic HAOS update decision.
type UpdatePlan struct {
	Supervisor Component `json:"supervisor"`
	Core       Component `json:"core"`
}

// UpdateResult summarizes an HAOS update run.
type UpdateResult struct {
	Updated []string
	Summary string
}

// New creates an HAOS client. baseURL is the Home Assistant Core URL.
func New(baseURL string, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTPClient: http.DefaultClient, PollInterval: 2 * time.Second}
}

// Verify confirms the endpoint exposes the HAOS Supervisor proxy.
func (c *Client) Verify(ctx context.Context) (Info, error) {
	supervisor, err := c.componentInfo(ctx, "supervisor")
	if err != nil {
		return Info{}, c.diagnoseSupervisorProxy(ctx, err)
	}
	core, err := c.componentInfo(ctx, "core")
	if err != nil {
		return Info{}, err
	}
	if supervisor.Version == "" || core.Version == "" {
		return Info{}, fmt.Errorf("%w: server did not respond like Home Assistant OS Supervisor", ErrAPI)
	}
	return Info{Supervisor: supervisor, Core: core}, nil
}

// CheckUpdates refreshes HAOS update data and returns Supervisor/Core decisions.
func (c *Client) CheckUpdates(ctx context.Context) (UpdatePlan, error) {
	if err := c.doJSON(ctx, http.MethodPost, "refresh_updates", nil, nil); err != nil {
		return UpdatePlan{}, err
	}
	info, err := c.Verify(ctx)
	if err != nil {
		return UpdatePlan{}, err
	}
	return UpdatePlan{Supervisor: withDecision(info.Supervisor), Core: withDecision(info.Core)}, nil
}

// ApplyUpdates updates Supervisor first, then Core, and never triggers a reboot.
func (c *Client) ApplyUpdates(ctx context.Context, report func(string)) (UpdateResult, error) {
	plan, err := c.CheckUpdates(ctx)
	if err != nil {
		return UpdateResult{}, err
	}
	updated := make([]string, 0, 2)
	steps := []Component{plan.Supervisor, plan.Core}
	for _, component := range steps {
		if !component.UpdateAvailable {
			reportLine(report, component.Name+" is already current ("+component.Version+")")
			continue
		}
		reportLine(report, "Updating "+component.Name+" from "+component.Version+" to "+component.LatestVersion)
		jobID, err := c.postUpdate(ctx, component.Name)
		if err != nil {
			return UpdateResult{}, err
		}
		if jobID != "" {
			reportLine(report, component.Name+" update job started: "+jobID)
			if err := c.waitJob(ctx, jobID, report); err != nil {
				return UpdateResult{}, err
			}
		}
		reportLine(report, component.Name+" update request completed")
		updated = append(updated, component.Name)
	}
	if len(updated) == 0 {
		return UpdateResult{Summary: "no updates available"}, nil
	}
	return UpdateResult{Updated: updated, Summary: "updated " + strings.Join(updated, ", ")}, nil
}

func (c *Client) componentInfo(ctx context.Context, name string) (Component, error) {
	var payload map[string]any
	if err := c.doJSON(ctx, http.MethodGet, name+"/info", nil, &payload); err != nil {
		return Component{}, err
	}
	data, ok := unwrapProxyData(payload)
	if !ok {
		return Component{}, fmt.Errorf("%w: server did not respond like Home Assistant OS Supervisor", ErrAPI)
	}
	component := Component{Name: name, Version: firstString(data, "version", "current_version"), LatestVersion: firstString(data, "version_latest", "latest_version", "newest_version")}
	component.UpdateAvailable = boolValue(data, "update_available") || boolValue(data, "available")
	return withDecision(component), nil
}

func (c *Client) postUpdate(ctx context.Context, name string) (string, error) {
	var payload any
	if err := c.doJSON(ctx, http.MethodPost, name+"/update", nil, &payload); err != nil {
		return "", err
	}
	return jobID(payload), nil
}

func (c *Client) waitJob(ctx context.Context, jobID string, report func(string)) error {
	interval := c.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	lastProgress := ""
	for {
		var payload map[string]any
		if err := c.doJSON(ctx, http.MethodGet, "jobs/"+jobID, nil, &payload); err != nil {
			return err
		}
		data := unwrapData(payload)
		progress := formatProgress(data)
		if progress != "" && progress != lastProgress {
			reportLine(report, progress)
			lastProgress = progress
		}
		state := strings.ToUpper(firstString(data, "state", "status"))
		switch state {
		case "SUCCESS", "FINISHED", "COMPLETED":
			reportLine(report, "HAOS job completed")
			return nil
		case "FAILED", "ERROR", "ABORTED":
			reason := firstString(data, "error", "message")
			if reason == "" {
				reason = "HAOS job " + strings.ToLower(state)
			}
			return fmt.Errorf("%w: %s", ErrAPI, reason)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
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
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAPI, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body := readErrorBody(resp.Body)
		return &HTTPError{Cause: ErrAuth, Method: method, Path: c.apiPath(path), Status: resp.Status, StatusCode: resp.StatusCode, Body: body}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body := readErrorBody(resp.Body)
		return &HTTPError{Cause: ErrAPI, Method: method, Path: c.apiPath(path), Status: resp.Status, StatusCode: resp.StatusCode, Body: body}
	}
	if output == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrAPI, err)
	}
	return nil
}

func (c *Client) url(path string) string {
	return c.BaseURL + c.apiPath(path)
}

func (c *Client) apiPath(path string) string {
	path = strings.TrimLeft(path, "/")
	if strings.HasSuffix(c.BaseURL, "/api/hassio") {
		return "/" + path
	}
	return "/api/hassio/" + path
}

func (c *Client) restAPIURL() string {
	base := strings.TrimSuffix(c.BaseURL, "/api/hassio")
	return strings.TrimRight(base, "/") + "/api/"
}

func (c *Client) diagnoseSupervisorProxy(ctx context.Context, err error) error {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound || !strings.HasPrefix(httpErr.Path, "/api/hassio/") {
		return err
	}
	if probeErr := c.probeRESTAPI(ctx); probeErr == nil {
		return fmt.Errorf("%w: %s: this Home Assistant instance does not expose the Supervisor proxy. HAOS mode requires Home Assistant OS or a Supervised install", ErrAPI, httpErr.Error())
	} else {
		return fmt.Errorf("%w: %s; Home Assistant REST probe at /api/ also failed: %v", ErrAPI, httpErr.Error(), probeErr)
	}
}

func (c *Client) probeRESTAPI(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restAPIURL(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAPI, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body := readErrorBody(resp.Body)
		path := "/api/"
		if parsed, err := url.Parse(c.restAPIURL()); err == nil {
			path = parsed.Path
		}
		return &HTTPError{Cause: ErrAPI, Method: http.MethodGet, Path: path, Status: resp.Status, StatusCode: resp.StatusCode, Body: body}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func readErrorBody(reader io.Reader) string {
	body, _ := io.ReadAll(io.LimitReader(reader, errorBodyLimit+1))
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) > errorBodyLimit {
		return trimmed[:errorBodyLimit]
	}
	return trimmed
}

func unwrapData(payload map[string]any) map[string]any {
	if nested, ok := payload["data"].(map[string]any); ok {
		return nested
	}
	return payload
}

func unwrapProxyData(payload map[string]any) (map[string]any, bool) {
	result, hasResult := payload["result"].(string)
	if hasResult && !strings.EqualFold(result, "ok") {
		return nil, false
	}
	nested, hasData := payload["data"].(map[string]any)
	if !hasResult || !hasData {
		return nil, false
	}
	return nested, true
}

func withDecision(component Component) Component {
	if component.LatestVersion != "" && component.Version != "" && component.LatestVersion != component.Version {
		component.UpdateAvailable = true
	}
	return component
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := values[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func boolValue(values map[string]any, key string) bool {
	switch value := values[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
	}
	return false
}

func jobID(payload any) string {
	switch value := payload.(type) {
	case map[string]any:
		data := value
		if nested, ok := value["data"].(map[string]any); ok {
			data = nested
		}
		for _, key := range []string{"job_id", "job", "id"} {
			if text, ok := data[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	case string:
		return strings.TrimSpace(value)
	}
	return ""
}

func formatProgress(data map[string]any) string {
	if progress, ok := data["progress"].(map[string]any); ok {
		description := firstString(progress, "description", "message")
		if percent, ok := progress["percent"].(float64); ok && description != "" {
			return fmt.Sprintf("HAOS progress: %.0f%% - %s", percent, description)
		}
		if description != "" {
			return "HAOS progress: " + description
		}
	}
	return firstString(data, "description", "message")
}

func reportLine(report func(string), line string) {
	if report == nil || strings.TrimSpace(line) == "" {
		return
	}
	report(strings.TrimRight(line, "\n") + "\n")
}
