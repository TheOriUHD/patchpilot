package core

import (
	"errors"
	"fmt"
	"strings"

	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
)

// BootstrapErrorCode identifies a user-facing bootstrap failure class.
type BootstrapErrorCode string

const (
	BootstrapErrorUnknown          BootstrapErrorCode = "unknown"
	BootstrapErrorSSHConnection    BootstrapErrorCode = "ssh_connection"
	BootstrapErrorDetectionProbe   BootstrapErrorCode = "detection_probe"
	BootstrapErrorCommandMissing   BootstrapErrorCode = "command_missing"
	BootstrapErrorAdminToolMissing BootstrapErrorCode = "admin_tool_missing"
	BootstrapErrorPublicKeyInvalid BootstrapErrorCode = "public_key_invalid"
	BootstrapErrorSudoInstall      BootstrapErrorCode = "sudo_install_failed"
	BootstrapErrorSudoersMissing   BootstrapErrorCode = "sudoers_missing"
	BootstrapErrorSudoersInclude   BootstrapErrorCode = "sudoers_include_missing"
	BootstrapErrorUserCreation     BootstrapErrorCode = "user_creation_failed"
	BootstrapErrorSSHDirectory     BootstrapErrorCode = "ssh_directory_failed"
	BootstrapErrorAuthorizedKeys   BootstrapErrorCode = "authorized_keys_failed"
	BootstrapErrorSudoersWrite     BootstrapErrorCode = "sudoers_write_failed"
	BootstrapErrorVisudo           BootstrapErrorCode = "visudo_failed"
	BootstrapErrorReconnect        BootstrapErrorCode = "patchpilot_reconnect_failed"
	BootstrapErrorNOPASSWD         BootstrapErrorCode = "nopasswd_verify_failed"
	BootstrapErrorDryRun           BootstrapErrorCode = "dry_run_failed"
	BootstrapErrorPackageLock      BootstrapErrorCode = "package_lock"
	BootstrapErrorNetwork          BootstrapErrorCode = "network_or_repo"
	BootstrapErrorPermission       BootstrapErrorCode = "permission_denied"
	BootstrapErrorSuAuth           BootstrapErrorCode = "su_auth_failed"
)

// BootstrapError is a classified bootstrap failure with operator guidance.
type BootstrapError struct {
	Step     string
	Code     BootstrapErrorCode
	Summary  string
	Guidance string
	Output   string
	Cause    error
}

func (e *BootstrapError) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{}
	if strings.TrimSpace(e.Summary) != "" {
		parts = append(parts, strings.TrimSpace(e.Summary))
	}
	if strings.TrimSpace(e.Guidance) != "" {
		parts = append(parts, strings.TrimSpace(e.Guidance))
	}
	if strings.TrimSpace(e.Output) != "" {
		parts = append(parts, "detail: "+strings.TrimSpace(e.Output))
	}
	if len(parts) == 0 && e.Cause != nil {
		return e.Cause.Error()
	}
	return strings.Join(parts, ": ")
}

func (e *BootstrapError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newBootstrapError(step string, code BootstrapErrorCode, summary string, guidance string, cause error, output string) error {
	return &BootstrapError{Step: step, Code: code, Summary: summary, Guidance: guidance, Cause: cause, Output: cleanBootstrapOutput(output)}
}

func bootstrapErrorDetails(err error) (BootstrapErrorCode, string) {
	var classified *BootstrapError
	if errors.As(err, &classified) {
		return classified.Code, classified.Guidance
	}
	return "", ""
}

func bootstrapErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func classifyBootstrapConnectionFailure(step string, err error) error {
	if err == nil {
		return nil
	}
	var classified *BootstrapError
	if errors.As(err, &classified) {
		return err
	}
	output := err.Error()
	lower := strings.ToLower(output)
	if strings.Contains(lower, "publickey") || strings.Contains(lower, "unable to authenticate") || strings.Contains(lower, "permission denied") {
		return newBootstrapError(step, BootstrapErrorReconnect, "patchpilot could not reconnect with its SSH key", "Check that PubkeyAuthentication is enabled and that /home/patchpilot/.ssh/authorized_keys is owned by patchpilot with 0600 permissions.", err, output)
	}
	return newBootstrapError(step, BootstrapErrorSSHConnection, "patchpilot could not connect to the host over SSH", "Check the hostname, SSH port, credentials, and network reachability, then retry.", err, output)
}

func ClassifyBootstrapFailure(step string, command string, result remotessh.CommandResult, err error) error {
	if err == nil {
		return nil
	}
	var classified *BootstrapError
	if errors.As(err, &classified) {
		return err
	}
	output := strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
	if output == "" {
		output = err.Error()
	}
	lower := strings.ToLower(output + "\n" + err.Error() + "\n" + step + "\n" + command)

	if suAuthenticationFailed(result, err) {
		return newBootstrapError(step, BootstrapErrorSuAuth, "su did not accept the root password", "Check the root password and retry automatic fix, or use pre-install mode.", err, output)
	}
	if strings.Contains(lower, "@includedir /etc/sudoers.d") || strings.Contains(lower, "#includedir /etc/sudoers.d") || strings.Contains(lower, "includedir /etc/sudoers.d") {
		return newBootstrapError(step, BootstrapErrorSudoersInclude, "sudo is installed, but /etc/sudoers does not include /etc/sudoers.d", "Add @includedir /etc/sudoers.d to /etc/sudoers manually, validate with visudo, then retry.", err, output)
	}
	if strings.Contains(lower, "missing /etc/sudoers") || strings.Contains(lower, "no such file or directory") && strings.Contains(lower, "/etc/sudoers") {
		return newBootstrapError(step, BootstrapErrorSudoersMissing, "/etc/sudoers is missing", "Install or repair sudo so /etc/sudoers exists, validate it with visudo, then retry.", err, output)
	}
	if missing := missingCommandFromResolverOutput(output); missing != "" {
		return newBootstrapError(step, BootstrapErrorCommandMissing, fmt.Sprintf("required command %s is missing", missing), "Install the missing command on the target host, then retry bootstrap.", err, output)
	}
	if strings.Contains(lower, "command not found") || strings.Contains(lower, "not found") && strings.Contains(lower, "visudo") {
		return newBootstrapError(step, BootstrapErrorAdminToolMissing, "a required bootstrap administration tool is missing", "Install the missing tool or sudo package on the target host, then retry.", err, output)
	}
	if packageLockFailure(lower) {
		return newBootstrapError(step, BootstrapErrorPackageLock, "the package manager is locked by another process", "Wait for the other package operation to finish, then retry bootstrap.", err, output)
	}
	if networkOrRepoFailure(lower) {
		return newBootstrapError(step, BootstrapErrorNetwork, "the package manager could not reach a repository", "Fix DNS, networking, repository configuration, or package availability on the host, then retry.", err, output)
	}
	if sudoPasswordRequired(output) || strings.Contains(lower, "a terminal is required") {
		return newBootstrapError(step, BootstrapErrorNOPASSWD, "patchpilot cannot run sudo without a password", "The sudoers drop-in is not being honored; check /etc/sudoers includes /etc/sudoers.d and retry repair/bootstrap.", err, output)
	}
	if strings.Contains(lower, "permission denied") {
		return newBootstrapError(step, BootstrapErrorPermission, "the bootstrap step was denied by the host", "Check filesystem permissions, sudo policy, and the SSH account's privileges, then retry.", err, output)
	}

	switch {
	case strings.Contains(strings.ToLower(step), "detection"):
		return newBootstrapError(step, BootstrapErrorDetectionProbe, "patchpilot could not read host facts", "Check that the target is a supported Linux host and that the SSH account can run basic shell commands.", err, output)
	case strings.Contains(strings.ToLower(step), "installing sudo"):
		return newBootstrapError(step, BootstrapErrorSudoInstall, "patchpilot could not install sudo", "Install sudo manually or fix package-manager access, then retry.", err, output)
	case strings.Contains(strings.ToLower(step), "creating system user"):
		return newBootstrapError(step, BootstrapErrorUserCreation, "patchpilot could not create or reuse the patchpilot user", "Check that user-management tools are installed and /home is writable, then retry.", err, output)
	case strings.Contains(strings.ToLower(step), ".ssh"):
		return newBootstrapError(step, BootstrapErrorSSHDirectory, "patchpilot could not prepare /home/patchpilot/.ssh", "Check the patchpilot home directory and filesystem permissions, then retry.", err, output)
	case strings.Contains(strings.ToLower(step), "public key") || strings.Contains(strings.ToLower(step), "authorized_keys"):
		return newBootstrapError(step, BootstrapErrorAuthorizedKeys, "patchpilot could not install its SSH public key", "Check /home/patchpilot/.ssh ownership and permissions, then retry.", err, output)
	case strings.Contains(strings.ToLower(step), "writing /etc/sudoers.d") || strings.Contains(strings.ToLower(step), "preparing /etc/sudoers.d"):
		return newBootstrapError(step, BootstrapErrorSudoersWrite, "patchpilot could not write the sudoers drop-in", "Check sudo installation, /etc/sudoers.d permissions, and /etc/sudoers include configuration, then retry.", err, output)
	case strings.Contains(strings.ToLower(step), "visudo") || strings.Contains(strings.ToLower(step), "validating sudoers"):
		return newBootstrapError(step, BootstrapErrorVisudo, "visudo rejected the patchpilot sudoers policy", "Review the sudoers error, remove /etc/sudoers.d/patchpilot if needed, and retry after sudoers is valid.", err, output)
	case strings.Contains(strings.ToLower(step), "reconnecting"):
		return newBootstrapError(step, BootstrapErrorReconnect, "patchpilot could not reconnect with its SSH key", "Check PubkeyAuthentication and patchpilot authorized_keys ownership/permissions, then retry.", err, output)
	case strings.Contains(strings.ToLower(step), "nopasswd"):
		return newBootstrapError(step, BootstrapErrorNOPASSWD, "patchpilot cannot use sudo without a password", "Check /etc/sudoers includes /etc/sudoers.d and that /etc/sudoers.d/patchpilot validates with visudo.", err, output)
	case strings.Contains(strings.ToLower(step), "dry run"):
		return newBootstrapError(step, BootstrapErrorDryRun, "the package-manager dry run failed", "Fix package-manager errors on the target host, then retry.", err, output)
	default:
		return newBootstrapError(step, BootstrapErrorUnknown, "bootstrap failed during "+step, "Review the detail output, fix the named precondition, then retry.", err, output)
	}
}

func packageLockFailure(lower string) bool {
	return strings.Contains(lower, "could not get lock") || strings.Contains(lower, "unable to acquire") || strings.Contains(lower, "unable to lock") || strings.Contains(lower, "is another process using")
}

func networkOrRepoFailure(lower string) bool {
	needles := []string{"temporary failure resolving", "could not resolve", "failed to fetch", "network is unreachable", "no route to host", "unable to locate package", "no package", "repository", "repo ", "404 not found"}
	for _, needle := range needles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func cleanBootstrapOutput(output string) string {
	trimmed := strings.TrimSpace(output)
	lower := strings.ToLower(trimmed)
	if trimmed == "" || lower == "exit status 1" || lower == "process exited with status 1" {
		return ""
	}
	if len(trimmed) > 500 {
		return trimmed[:500] + "..."
	}
	return trimmed
}
