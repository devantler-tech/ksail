package sshbootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	// DefaultDialTimeout bounds a single TCP+handshake attempt when
	// Options.DialTimeout is zero.
	DefaultDialTimeout = 10 * time.Second

	// DefaultRetryInterval is the pause between [DialWithRetry] attempts when the
	// caller passes a non-positive interval. First boot of a cloud server takes
	// tens of seconds, so a few seconds between probes is plenty.
	DefaultRetryInterval = 3 * time.Second
)

// Options configures [Dial]. Addr, User, Signer, and HostKeyCallback are
// required; see the matching Err* sentinels.
type Options struct {
	// Addr is the server's host:port SSH endpoint.
	Addr string
	// User is the login to authenticate as (root on Hetzner stock images).
	User string
	// Signer is the client identity, typically [KeyPair.Signer].
	Signer ssh.Signer
	// HostKeyCallback is the host-key trust policy. There is no default: pick
	// ssh.FixedHostKey when the host key is known, or an explicit
	// trust-on-first-use policy at the call site.
	HostKeyCallback ssh.HostKeyCallback
	// DialTimeout bounds each connection attempt; zero means
	// [DefaultDialTimeout].
	DialTimeout time.Duration
}

func (o Options) validate() error {
	switch {
	case o.Addr == "":
		return ErrMissingAddr
	case o.User == "":
		return ErrMissingUser
	case o.Signer == nil:
		return ErrMissingSigner
	case o.HostKeyCallback == nil:
		return ErrMissingHostKeyCallback
	default:
		return nil
	}
}

// Client is an established SSH connection to a bootstrapped server. It is safe
// for sequential use; close it when done.
type Client struct {
	conn *ssh.Client
}

// RunResult carries a remote command's captured output. ExitCode is only
// meaningful when the returned error wraps [ErrCommandFailed] (non-zero exit)
// or is nil (zero exit).
type RunResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Dial opens a single SSH connection attempt to the server, bounded by ctx and
// Options.DialTimeout.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	err := opts.validate()
	if err != nil {
		return nil, err
	}

	timeout := opts.DialTimeout
	if timeout <= 0 {
		timeout = DefaultDialTimeout
	}

	dialer := net.Dialer{Timeout: timeout}

	netConn, err := dialer.DialContext(ctx, "tcp", opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", opts.Addr, err)
	}

	config := &ssh.ClientConfig{
		User:            opts.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(opts.Signer)},
		HostKeyCallback: opts.HostKeyCallback,
		Timeout:         timeout,
	}

	// The handshake API is not context-aware; closing the net.Conn on
	// cancellation unblocks it.
	stop := closeOnDone(ctx, netConn)

	sshConn, channels, requests, err := ssh.NewClientConn(netConn, opts.Addr, config)

	stop()

	if err != nil {
		_ = netConn.Close()

		return nil, fmt.Errorf("ssh handshake with %s: %w", opts.Addr, err)
	}

	return &Client{conn: ssh.NewClient(sshConn, channels, requests)}, nil
}

// DialWithRetry dials until the server's SSH endpoint accepts the connection
// or ctx expires — the "wait for first boot" primitive. A non-positive
// interval means [DefaultRetryInterval]. The last attempt's error is wrapped
// into the context error so the caller sees why the wait was still failing
// when time ran out.
func DialWithRetry(
	ctx context.Context,
	opts Options,
	interval time.Duration,
) (*Client, error) {
	if interval <= 0 {
		interval = DefaultRetryInterval
	}

	var lastErr error

	for {
		client, err := Dial(ctx, opts)
		if err == nil {
			return client, nil
		}

		// Option errors never heal; retrying them would just burn the deadline.
		if errors.Is(err, ErrMissingAddr) || errors.Is(err, ErrMissingUser) ||
			errors.Is(err, ErrMissingSigner) || errors.Is(err, ErrMissingHostKeyCallback) {
			return nil, err
		}

		lastErr = err

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf(
				"ssh endpoint %s did not come up: %w (last attempt: %w)",
				opts.Addr, ctx.Err(), lastErr,
			)
		case <-time.After(interval):
		}
	}
}

// Run executes a single command on the server and captures its output. A
// non-zero exit returns [RunResult] alongside an error wrapping
// [ErrCommandFailed]; transport failures return only the error.
func (c *Client) Run(ctx context.Context, command string) (RunResult, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return RunResult{}, fmt.Errorf("open session: %w", err)
	}

	defer func() { _ = session.Close() }()

	var stdout, stderr bytes.Buffer

	session.Stdout = &stdout
	session.Stderr = &stderr

	// Session.Run is not context-aware; closing the session on cancellation
	// unblocks it.
	stop := closeOnDone(ctx, session)

	err = session.Run(command)

	stop()

	result := RunResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0}

	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitStatus()

		return result, fmt.Errorf(
			"%w: %q exited %d: %s",
			ErrCommandFailed, command, result.ExitCode,
			strings.TrimSpace(stderr.String()),
		)
	}

	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("run %q: %w", command, ctx.Err())
		}

		return result, fmt.Errorf("run %q: %w", command, err)
	}

	return result, nil
}

// ReadFile streams a remote file's contents over the exec channel (`cat`), so
// fetching the kubeconfig needs no SFTP subsystem. The path is single-quoted
// against shell interpretation.
func (c *Client) ReadFile(ctx context.Context, path string) ([]byte, error) {
	result, err := c.Run(ctx, "cat -- "+shellQuote(path))
	if err != nil {
		return nil, fmt.Errorf("read remote file %s: %w", path, err)
	}

	return result.Stdout, nil
}

// Close tears down the underlying SSH connection.
func (c *Client) Close() error {
	err := c.conn.Close()
	if err != nil {
		return fmt.Errorf("close ssh connection: %w", err)
	}

	return nil
}

// closeOnDone closes closer when ctx is cancelled, and returns a stop func
// that ends the watch (idempotent with respect to the ctx firing afterwards —
// the closer is only closed while the watch is active).
func closeOnDone(ctx context.Context, closer interface{ Close() error }) func() {
	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			_ = closer.Close()
		case <-done:
		}
	}()

	return func() { close(done) }
}

// shellQuote single-quotes s for POSIX shells, escaping embedded single
// quotes, so remote paths survive word splitting and globbing.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
