package truenas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const CoreUnsupportedMessage = "TrueNAS Core is not supported. Use TrueNAS SCALE."

var (
	// ErrAuth is returned when the API rejects the supplied token.
	ErrAuth = errors.New("auth failed")
	// ErrAPI is returned when the server does not behave like the expected API.
	ErrAPI = errors.New("API call failed")
)

// Client talks to the TrueNAS SCALE v2 REST API.
type Client struct {
	BaseURL      string
	Token        string
	HTTPClient   *http.Client
	PollInterval time.Duration
}

// Info contains display facts from /system/info.
type Info struct {
	Version string `json:"version"`
	Product string `json:"product"`
}

// CheckResult is the update/check_available response.
type CheckResult struct {
	Status    string `json:"status"`
	Changelog string `json:"changelog"`
}

// UpdateResult is the result of applying a TrueNAS update.
type UpdateResult struct {
	Applied        bool
	RebootRequired bool
	Summary        string
}

// New creates a TrueNAS client.
func New(baseURL string, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTPClient: http.DefaultClient, PollInterval: 2 * time.Second}
}

// Verify checks that the endpoint is TrueNAS SCALE and returns its version.
func (c *Client) Verify(ctx context.Context) (Info, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "/api/v2.0/system/info", nil, &raw); err != nil {
		return Info{}, err
	}
	info := Info{Version: stringValue(raw, "version"), Product: firstStringValue(raw, "product", "system_product", "system_product_name")}
	marker := strings.ToUpper(strings.Join([]string{info.Version, info.Product}, " "))
	if strings.Contains(marker, "SCALE") {
		return info, nil
	}
	if strings.Contains(marker, "CORE") || strings.Contains(marker, "TRUENAS") {
		return Info{}, errors.New(CoreUnsupportedMessage)
	}
	return Info{}, fmt.Errorf("%w: server did not respond like TrueNAS SCALE", ErrAPI)
}

// CheckUpdates checks whether updates are available.
func (c *Client) CheckUpdates(ctx context.Context) (CheckResult, error) {
	var result CheckResult
	if err := c.doJSON(ctx, http.MethodPost, "/api/v2.0/update/check_available", map[string]any{}, &result); err != nil {
		return CheckResult{}, err
	}
	result.Status = strings.ToUpper(strings.TrimSpace(result.Status))
	if result.Status == "" {
		return CheckResult{}, fmt.Errorf("%w: missing update status", ErrAPI)
	}
	return result, nil
}

// ApplyUpdate applies an available update, never requesting automatic reboot.
func (c *Client) ApplyUpdate(ctx context.Context, report func(string)) (UpdateResult, error) {
	check, err := c.CheckUpdates(ctx)
	if err != nil {
		return UpdateResult{}, err
	}
	if check.Status != "AVAILABLE" {
		reportLine(report, "TrueNAS reports no updates available")
		return UpdateResult{Summary: "no updates available"}, nil
	}
	if check.Changelog != "" {
		reportLine(report, "TrueNAS update available")
		reportLine(report, check.Changelog)
	}
	var raw any
	if err := c.doJSON(ctx, http.MethodPost, "/api/v2.0/update/update", map[string]any{"reboot": false}, &raw); err != nil {
		return UpdateResult{}, err
	}
	jobID, err := parseJobID(raw)
	if err != nil {
		return UpdateResult{}, err
	}
	reportLine(report, "TrueNAS update job started: "+jobID)
	job, err := c.waitJob(ctx, jobID, report)
	if err != nil {
		return UpdateResult{}, err
	}
	rebootRequired := containsRebootRequired(job.Result) || containsRebootRequired(job.Progress.Extra) || stringValue(job.Raw, "reboot_required") == "true"
	if rebootRequired {
		reportLine(report, "TrueNAS reports a reboot is required; patchpilot will not reboot automatically")
	}
	return UpdateResult{Applied: true, RebootRequired: rebootRequired, Summary: "update completed"}, nil
}

type jobRecord struct {
	ID       any            `json:"id"`
	State    string         `json:"state"`
	Error    string         `json:"error"`
	Result   any            `json:"result"`
	Progress jobProgress    `json:"progress"`
	Raw      map[string]any `json:"-"`
}

type jobProgress struct {
	Percent     float64 `json:"percent"`
	Description string  `json:"description"`
	Extra       any     `json:"extra"`
}

func (c *Client) waitJob(ctx context.Context, jobID string, report func(string)) (jobRecord, error) {
	interval := c.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	lastProgress := ""
	for {
		job, err := c.getJob(ctx, jobID)
		if err != nil {
			return jobRecord{}, err
		}
		progress := formatProgress(job)
		if progress != "" && progress != lastProgress {
			reportLine(report, progress)
			lastProgress = progress
		}
		switch strings.ToUpper(job.State) {
		case "SUCCESS":
			reportLine(report, "TrueNAS update job completed")
			return job, nil
		case "FAILED", "ABORTED":
			if job.Error == "" {
				job.Error = "TrueNAS update job " + strings.ToLower(job.State)
			}
			return jobRecord{}, fmt.Errorf("%w: %s", ErrAPI, job.Error)
		}
		select {
		case <-ctx.Done():
			return jobRecord{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func (c *Client) getJob(ctx context.Context, jobID string) (jobRecord, error) {
	var jobs []jobRecord
	path := "/api/v2.0/core/get_jobs?id=" + url.QueryEscape(jobID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &jobs); err != nil {
		return jobRecord{}, err
	}
	if len(jobs) == 0 {
		return jobRecord{}, fmt.Errorf("%w: update job %s not found", ErrAPI, jobID)
	}
	var raw map[string]any
	buffer, _ := json.Marshal(jobs[0])
	_ = json.Unmarshal(buffer, &raw)
	jobs[0].Raw = raw
	return jobs[0], nil
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
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
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
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, resp.Body)
		return ErrAuth
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%w: %s %s", ErrAPI, resp.Status, strings.TrimSpace(string(body)))
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

func parseJobID(raw any) (string, error) {
	switch value := raw.(type) {
	case float64:
		return strconv.FormatInt(int64(value), 10), nil
	case string:
		if strings.TrimSpace(value) != "" {
			return value, nil
		}
	case map[string]any:
		for _, key := range []string{"job_id", "id", "job"} {
			if id := stringify(value[key]); id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("%w: update response did not include a job id", ErrAPI)
}

func formatProgress(job jobRecord) string {
	parts := []string{}
	if job.Progress.Percent > 0 {
		parts = append(parts, fmt.Sprintf("%.0f%%", job.Progress.Percent))
	}
	if strings.TrimSpace(job.Progress.Description) != "" {
		parts = append(parts, strings.TrimSpace(job.Progress.Description))
	}
	if len(parts) == 0 {
		return ""
	}
	return "TrueNAS progress: " + strings.Join(parts, " - ")
}

func containsRebootRequired(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.Contains(strings.ToLower(typed), "reboot")
	case map[string]any:
		for key, nested := range typed {
			lowerKey := strings.ToLower(key)
			if (lowerKey == "reboot_required" || lowerKey == "requires_reboot") && truthy(nested) {
				return true
			}
			if containsRebootRequired(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsRebootRequired(nested) {
				return true
			}
		}
	}
	return false
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true") || strings.EqualFold(typed, "yes")
	}
	return false
}

func stringValue(values map[string]any, key string) string {
	return stringify(values[key])
}

func firstStringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringify(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringify(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	}
	return ""
}

func reportLine(report func(string), line string) {
	if report == nil || strings.TrimSpace(line) == "" {
		return
	}
	report(strings.TrimRight(line, "\n") + "\n")
}
