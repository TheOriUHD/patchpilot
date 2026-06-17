package core

import (
	"context"
	"fmt"
	"strings"

	remotessh "github.com/TheOriUHD/patchpilot/internal/ssh"
)

type escalationMode string

const (
	escalationDirectRoot   escalationMode = "direct-root"
	escalationSudoNoPasswd escalationMode = "sudo-nopasswd"
	escalationSudoPassword escalationMode = "sudo-password"
	escalationSuRoot       escalationMode = "su-root"
)

type escalationInput struct {
	Facts        HostFacts
	Password     []byte
	RootPassword []byte
}

type escalatedCommand struct {
	Command string
	Stdin   []byte
	Options remotessh.RunOptions
	Mode    escalationMode
}

func buildEscalatedCommand(input escalationInput, command string) (escalatedCommand, error) {
	mode, err := chooseEscalationMode(input)
	if err != nil {
		return escalatedCommand{}, err
	}
	switch mode {
	case escalationDirectRoot:
		return escalatedCommand{Command: "sh -c " + shellQuote(command), Mode: mode}, nil
	case escalationSudoNoPasswd:
		return escalatedCommand{Command: "sudo -n sh -c " + shellQuote(command), Mode: mode}, nil
	case escalationSudoPassword:
		stdin := append([]byte{}, input.Password...)
		stdin = append(stdin, '\n')
		return escalatedCommand{Command: "sudo -S -p '' sh -c " + shellQuote(command), Stdin: stdin, Mode: mode}, nil
	case escalationSuRoot:
		stdin := append([]byte{}, input.RootPassword...)
		return escalatedCommand{Command: "su -c " + shellQuote(command), Stdin: stdin, Options: remotessh.RunOptions{PTY: true}, Mode: mode}, nil
	default:
		return escalatedCommand{}, fmt.Errorf("unsupported escalation mode %q", mode)
	}
}

func chooseEscalationMode(input escalationInput) (escalationMode, error) {
	if input.Facts.CurrentUID == "0" {
		return escalationDirectRoot, nil
	}
	if input.Facts.SudoNoPassword {
		return escalationSudoNoPasswd, nil
	}
	if input.Facts.SudoPresent && len(input.Password) > 0 {
		return escalationSudoPassword, nil
	}
	if input.Facts.SuPresent && len(input.RootPassword) > 0 {
		return escalationSuRoot, nil
	}
	if !input.Facts.SudoPresent && !input.Facts.SuPresent {
		return "", fmt.Errorf("sudo is not installed and su is unavailable; use pre-install mode")
	}
	if !input.Facts.SudoPresent {
		return "", fmt.Errorf("sudo is not installed; provide the root password so patchpilot can use su to install sudo")
	}
	return "", fmt.Errorf("the SSH user cannot run sudo non-interactively; provide the root password so patchpilot can repair sudoers with su")
}

func runEscalated(ctx context.Context, runner remotessh.Runner, escalation escalationInput, command string) (remotessh.CommandResult, escalatedCommand, error) {
	escalated, err := buildEscalatedCommand(escalation, command)
	if err != nil {
		return remotessh.CommandResult{}, escalatedCommand{}, err
	}
	result, runErr := runRunnerCommand(ctx, runner, escalated)
	return result, escalated, runErr
}

func runRunnerCommand(ctx context.Context, runner remotessh.Runner, command escalatedCommand) (remotessh.CommandResult, error) {
	defer ZeroBytes(command.Stdin)
	if command.Options.PTY {
		if optionsRunner, ok := runner.(interface {
			RunWithOptions(context.Context, string, []byte, remotessh.RunOptions) (remotessh.CommandResult, error)
		}); ok {
			return optionsRunner.RunWithOptions(ctx, command.Command, command.Stdin, command.Options)
		}
	}
	return runner.Run(ctx, command.Command, command.Stdin)
}

func suAuthenticationFailed(result remotessh.CommandResult, err error) bool {
	if err == nil {
		return false
	}
	output := strings.ToLower(result.Stdout + "\n" + result.Stderr + "\n" + err.Error())
	trimmedOutput := strings.TrimSpace(strings.ToLower(result.Stdout + "\n" + result.Stderr))
	return strings.Contains(output, "authentication failure") || strings.Contains(output, "authentication failed") || strings.Contains(output, "incorrect password") || strings.Contains(output, "permission denied") || trimmedOutput == "password:"
}
