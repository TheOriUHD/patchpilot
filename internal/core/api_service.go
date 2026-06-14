package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TheOriUHD/patchpilot/internal/haos"
	"github.com/TheOriUHD/patchpilot/internal/pkgmgr"
	"github.com/TheOriUHD/patchpilot/internal/truenas"
)

// SecretCipher encrypts and decrypts stored API tokens.
type SecretCipher interface {
	EncryptString(ctx context.Context, plaintext string) (string, string, error)
	DecryptString(ctx context.Context, ciphertext string, nonce string) (string, error)
}

// HostAPITokenRepository retrieves encrypted API tokens for API-managed hosts.
type HostAPITokenRepository interface {
	GetAPIToken(ctx context.Context, id int64) (EncryptedSecret, error)
}

// APIHostVerifyInput contains endpoint data for a native API host.
type APIHostVerifyInput struct {
	Type     HostType `json:"host_type"`
	Endpoint string   `json:"endpoint"`
	APIToken []byte   `json:"-"`
}

// APIHostCommitInput saves an API-managed host.
type APIHostCommitInput struct {
	Type            HostType `json:"host_type"`
	Endpoint        string   `json:"endpoint"`
	APIToken        []byte   `json:"-"`
	Name            string   `json:"name"`
	Notes           string   `json:"notes"`
	Enabled         bool     `json:"enabled"`
	AutoReboot      bool     `json:"auto_reboot"`
	AutoRebootDelay int      `json:"auto_reboot_countdown_seconds"`
}

// APIComponentStatus describes one HAOS component in the wizard review.
type APIComponentStatus struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

// APIHostVerifyResult is used by API verification and update-check wizard steps.
type APIHostVerifyResult struct {
	Type            HostType             `json:"host_type"`
	Endpoint        string               `json:"endpoint"`
	Version         string               `json:"version"`
	LastCheckStatus string               `json:"last_check_status"`
	Changelog       string               `json:"changelog"`
	Components      []APIComponentStatus `json:"components,omitempty"`
	Diagnosis       string               `json:"diagnosis,omitempty"`
	Refusal         string               `json:"refusal,omitempty"`
}

// VerifyAPIHost verifies the selected API surface and token.
func (s *HostService) VerifyAPIHost(ctx context.Context, input APIHostVerifyInput) (APIHostVerifyResult, error) {
	endpoint := normalizeAPIEndpoint(input.Endpoint)
	result := APIHostVerifyResult{Type: input.Type, Endpoint: endpoint}
	switch input.Type {
	case HostTypeTrueNAS:
		client := truenas.New(endpoint, string(input.APIToken))
		info, err := client.Verify(ctx)
		if err != nil {
			if err.Error() == truenas.CoreUnsupportedMessage {
				result.Refusal = truenas.CoreUnsupportedMessage
				return result, nil
			}
			result.Diagnosis = apiDiagnosis(err)
			return result, nil
		}
		result.Version = info.Version
		return result, nil
	case HostTypeHAOS:
		client := haos.New(endpoint, string(input.APIToken))
		info, err := client.Verify(ctx)
		if err != nil {
			result.Diagnosis = apiDiagnosis(err)
			return result, nil
		}
		result.Version = "Supervisor " + info.Supervisor.Version + " / Core " + info.Core.Version
		result.Components = haosComponents(info.Supervisor, info.Core)
		return result, nil
	default:
		return APIHostVerifyResult{}, fmt.Errorf("unsupported API host type %q", input.Type)
	}
}

// CheckAPIHost performs the wizard dry-run/update-check step.
func (s *HostService) CheckAPIHost(ctx context.Context, input APIHostVerifyInput) (APIHostVerifyResult, error) {
	endpoint := normalizeAPIEndpoint(input.Endpoint)
	result := APIHostVerifyResult{Type: input.Type, Endpoint: endpoint}
	switch input.Type {
	case HostTypeTrueNAS:
		client := truenas.New(endpoint, string(input.APIToken))
		info, err := client.Verify(ctx)
		if err != nil {
			if err.Error() == truenas.CoreUnsupportedMessage {
				result.Refusal = truenas.CoreUnsupportedMessage
				return result, nil
			}
			result.Diagnosis = apiDiagnosis(err)
			return result, nil
		}
		check, err := client.CheckUpdates(ctx)
		if err != nil {
			result.Diagnosis = apiDiagnosis(err)
			return result, nil
		}
		result.Version = info.Version
		result.LastCheckStatus = check.Status
		result.Changelog = check.Changelog
		return result, nil
	case HostTypeHAOS:
		client := haos.New(endpoint, string(input.APIToken))
		plan, err := client.CheckUpdates(ctx)
		if err != nil {
			result.Diagnosis = apiDiagnosis(err)
			return result, nil
		}
		result.Version = "Supervisor " + plan.Supervisor.Version + " / Core " + plan.Core.Version
		result.Components = haosComponents(plan.Supervisor, plan.Core)
		result.LastCheckStatus = "UNAVAILABLE"
		for _, component := range result.Components {
			if component.UpdateAvailable {
				result.LastCheckStatus = "AVAILABLE"
				break
			}
		}
		return result, nil
	default:
		return APIHostVerifyResult{}, fmt.Errorf("unsupported API host type %q", input.Type)
	}
}

// CommitAPIHost verifies, encrypts, and saves an API-managed host.
func (s *HostService) CommitAPIHost(ctx context.Context, input APIHostCommitInput) (Host, error) {
	if s.Secrets == nil {
		return Host{}, fmt.Errorf("API token encryption is not configured")
	}
	verify, err := s.VerifyAPIHost(ctx, APIHostVerifyInput{Type: input.Type, Endpoint: input.Endpoint, APIToken: input.APIToken})
	if err != nil {
		return Host{}, err
	}
	if verify.Refusal != "" {
		return Host{}, errors.New(verify.Refusal)
	}
	if verify.Diagnosis != "" {
		return Host{}, errors.New(verify.Diagnosis)
	}
	ciphertext, nonce, err := s.Secrets.EncryptString(ctx, string(input.APIToken))
	if err != nil {
		return Host{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = defaultAPIHostName(input.Type, verify.Endpoint)
	}
	create := HostCreate{
		Name:               name,
		Hostname:           verify.Endpoint,
		SSHPort:            443,
		SSHUser:            "api",
		Type:               input.Type,
		PackageManager:     apiPackageManager(input.Type),
		OSPretty:           apiOSPretty(input.Type, verify.Version),
		Enabled:            input.Enabled,
		AutoReboot:         input.AutoReboot,
		AutoRebootDelay:    NormalizeRebootDelay(input.AutoRebootDelay),
		Notes:              input.Notes,
		APITokenCiphertext: ciphertext,
		APITokenNonce:      nonce,
	}
	host, err := s.Hosts.Create(ctx, create)
	if err != nil {
		return Host{}, err
	}
	s.audit(ctx, "tui", "host.add", "host", &host.ID, map[string]any{"name": host.Name, "host_type": host.Type})
	return host, nil
}

func (s *HostService) dryRunTrueNAS(ctx context.Context, host Host) (pkgmgr.DryRunResult, error) {
	token, err := s.decryptHostAPIToken(ctx, host.ID)
	if err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	client := truenas.New(host.Hostname, token)
	if _, err := client.Verify(ctx); err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	check, err := client.CheckUpdates(ctx)
	if err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	upgrades := 0
	if check.Status == "AVAILABLE" {
		upgrades = 1
	}
	return pkgmgr.DryRunResult{PackagesUpgraded: upgrades, Summary: "TrueNAS update status: " + check.Status, RawOutput: check.Changelog}, nil
}

func (s *HostService) dryRunHAOS(ctx context.Context, host Host) (pkgmgr.DryRunResult, error) {
	token, err := s.decryptHostAPIToken(ctx, host.ID)
	if err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	client := haos.New(host.Hostname, token)
	plan, err := client.CheckUpdates(ctx)
	if err != nil {
		return pkgmgr.DryRunResult{}, err
	}
	upgrades := 0
	components := []string{}
	for _, component := range []haos.Component{plan.Supervisor, plan.Core} {
		if component.UpdateAvailable {
			upgrades++
			components = append(components, component.Name)
		}
	}
	summary := "HAOS components are current"
	if upgrades > 0 {
		summary = "HAOS updates available: " + strings.Join(components, ", ")
	}
	return pkgmgr.DryRunResult{PackagesUpgraded: upgrades, Summary: summary, RawOutput: summary}, nil
}

func (s *HostService) decryptHostAPIToken(ctx context.Context, hostID int64) (string, error) {
	if s.Secrets == nil {
		return "", fmt.Errorf("API token encryption is not configured")
	}
	tokenStore, ok := s.Hosts.(HostAPITokenRepository)
	if !ok {
		return "", fmt.Errorf("host token storage is not configured")
	}
	encrypted, err := tokenStore.GetAPIToken(ctx, hostID)
	if err != nil {
		return "", err
	}
	return s.Secrets.DecryptString(ctx, encrypted.Ciphertext, encrypted.Nonce)
}

func normalizeAPIEndpoint(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}

func apiDiagnosis(err error) string {
	if errors.Is(err, truenas.ErrAuth) || errors.Is(err, haos.ErrAuth) {
		return "auth failed"
	}
	return "API call failed"
}

func haosComponents(components ...haos.Component) []APIComponentStatus {
	statuses := make([]APIComponentStatus, 0, len(components))
	for _, component := range components {
		statuses = append(statuses, APIComponentStatus{Name: component.Name, Version: component.Version, LatestVersion: component.LatestVersion, UpdateAvailable: component.UpdateAvailable})
	}
	return statuses
}

func apiPackageManager(hostType HostType) string {
	switch hostType {
	case HostTypeTrueNAS:
		return string(pkgmgr.ManagerTrueNASAPI)
	case HostTypeHAOS:
		return string(pkgmgr.ManagerHAOSAPI)
	default:
		return ""
	}
}

func apiOSPretty(hostType HostType, version string) string {
	switch hostType {
	case HostTypeTrueNAS:
		if version == "" {
			return "TrueNAS SCALE"
		}
		return "TrueNAS SCALE " + version
	case HostTypeHAOS:
		if version == "" {
			return "Home Assistant OS"
		}
		return "Home Assistant OS " + version
	default:
		return version
	}
}

func defaultAPIHostName(hostType HostType, endpoint string) string {
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	endpoint = strings.Trim(endpoint, "/")
	switch hostType {
	case HostTypeTrueNAS:
		return "truenas-" + endpoint
	case HostTypeHAOS:
		return "haos-" + endpoint
	default:
		return endpoint
	}
}
