package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/TheOriUHD/patchpilot/internal/diagnostics"
	xssh "golang.org/x/crypto/ssh"
)

// CommandResult is the captured result of a remote command.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// StreamChunk is a piece of stdout or stderr emitted by a remote command.
type StreamChunk struct {
	Stream string
	Data   []byte
}

// RunOptions configures one remote command execution.
type RunOptions struct {
	PTY bool
}

// Runner executes commands on a remote host.
type Runner interface {
	Run(ctx context.Context, command string, stdin []byte) (CommandResult, error)
	Close() error
}

// Client is an SSH-backed remote command runner.
type Client struct {
	client *xssh.Client
}

// ConnectPassword opens an SSH connection using password authentication.
func ConnectPassword(ctx context.Context, host string, port int, user string, password []byte) (*Client, error) {
	config := &xssh.ClientConfig{
		User:            user,
		Auth:            []xssh.AuthMethod{xssh.Password(string(password))},
		HostKeyCallback: xssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	return connect(ctx, host, port, config)
}

// ConnectKey opens an SSH connection using a private key signer.
func ConnectKey(ctx context.Context, host string, port int, user string, signer xssh.Signer) (*Client, error) {
	config := &xssh.ClientConfig{
		User:            user,
		Auth:            []xssh.AuthMethod{xssh.PublicKeys(signer)},
		HostKeyCallback: xssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	return connect(ctx, host, port, config)
}

// Run executes a command and captures stdout, stderr, and exit code.
func (c *Client) Run(ctx context.Context, command string, stdin []byte) (CommandResult, error) {
	return c.RunWithOptions(ctx, command, stdin, RunOptions{})
}

// RunWithOptions executes a command with optional SSH session settings.
func (c *Client) RunWithOptions(ctx context.Context, command string, stdin []byte, options RunOptions) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}

	session, err := c.client.NewSession()
	if err != nil {
		return CommandResult{}, fmt.Errorf("open ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if options.PTY {
		if err := session.RequestPty("xterm", 80, 24, xssh.TerminalModes{xssh.ECHO: 0}); err != nil {
			return CommandResult{}, fmt.Errorf("request ssh pty: %w", err)
		}
	}
	if len(stdin) > 0 {
		session.Stdin = bytes.NewReader(stdin)
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(xssh.SIGKILL)
		_ = session.Close()
		return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1}, ctx.Err()
	case err := <-done:
		result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode(err)}
		if err != nil {
			return result, fmt.Errorf("run remote command: %w", err)
		}
		return result, nil
	}
}

// RunStream executes a command while reporting stdout and stderr chunks as they arrive.
func (c *Client) RunStream(ctx context.Context, command string, stdin []byte, onChunk func(StreamChunk)) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}

	session, err := c.client.NewSession()
	if err != nil {
		return CommandResult{}, fmt.Errorf("open ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return CommandResult{}, fmt.Errorf("open stdout pipe: %w", err)
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return CommandResult{}, fmt.Errorf("open stderr pipe: %w", err)
	}
	if len(stdin) > 0 {
		session.Stdin = bytes.NewReader(stdin)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var waitGroup sync.WaitGroup
	copyPipe := func(name string, reader io.Reader, buffer *bytes.Buffer) {
		defer waitGroup.Done()
		_, _ = io.Copy(streamWriter{stream: name, buffer: buffer, onChunk: onChunk}, reader)
	}

	if err := session.Start(command); err != nil {
		return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode(err)}, fmt.Errorf("start remote command: %w", err)
	}
	waitGroup.Add(2)
	go copyPipe("stdout", stdoutPipe, &stdout)
	go copyPipe("stderr", stderrPipe, &stderr)

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(xssh.SIGKILL)
		_ = session.Close()
		waitGroup.Wait()
		return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1}, ctx.Err()
	case err := <-done:
		waitGroup.Wait()
		result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode(err)}
		if err != nil {
			return result, fmt.Errorf("run remote command: %w", err)
		}
		return result, nil
	}
}

// Close closes the SSH connection.
func (c *Client) Close() error {
	diagnostics.BootstrapDebugf("ssh.Client.Close: before close")
	err := c.client.Close()
	diagnostics.BootstrapDebugf("ssh.Client.Close: after close err=%v", err)
	return err
}

func connect(ctx context.Context, host string, port int, config *xssh.ClientConfig) (*Client, error) {
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial ssh %s: %w", address, err)
	}

	done := make(chan struct {
		client *xssh.Client
		err    error
	}, 1)
	go func() {
		sshConn, channels, requests, err := xssh.NewClientConn(conn, address, config)
		if err != nil {
			done <- struct {
				client *xssh.Client
				err    error
			}{err: err}
			return
		}
		done <- struct {
			client *xssh.Client
			err    error
		}{client: xssh.NewClient(sshConn, channels, requests)}
	}()

	select {
	case <-ctx.Done():
		_ = conn.Close()
		return nil, ctx.Err()
	case result := <-done:
		if result.err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("handshake ssh %s: %w", address, result.err)
		}
		return &Client{client: result.client}, nil
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *xssh.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitStatus()
	}
	return -1
}

type streamWriter struct {
	stream  string
	buffer  *bytes.Buffer
	onChunk func(StreamChunk)
}

func (w streamWriter) Write(data []byte) (int, error) {
	_, _ = w.buffer.Write(data)
	if w.onChunk != nil && len(data) > 0 {
		chunk := append([]byte(nil), data...)
		w.onChunk(StreamChunk{Stream: w.stream, Data: chunk})
	}
	return len(data), nil
}
