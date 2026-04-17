package eksctl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// DefaultBinary is the default binary name used to locate eksctl on PATH.
const DefaultBinary = "eksctl"

// Runner executes an external command and returns its stdout and stderr.
// Exposed as an interface so tests can inject a fake without relying on the
// eksctl binary being installed.
type Runner interface {
	Run(
		ctx context.Context,
		name string,
		args []string,
		stdin io.Reader,
	) (stdout, stderr []byte, err error)
}

// ExecRunner is the default Runner that shells out via os/exec.
type ExecRunner struct{}

// Run executes the given command using os/exec and returns the collected
// stdout and stderr buffers.
func (ExecRunner) Run(
	ctx context.Context,
	name string,
	args []string,
	stdin io.Reader,
) ([]byte, []byte, error) {
	// #nosec G204 -- name/args are constructed by our own eksctl client code;
	// no untrusted user input flows into these values.
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if stdin != nil {
		cmd.Stdin = stdin
	}

	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("%w: %w", ErrExecFailed, err)
	}

	return stdout.Bytes(), stderr.Bytes(), nil
}

// Client is the eksctl CLI wrapper.
type Client struct {
	binary string
	runner Runner
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithBinary overrides the binary name/path used to invoke eksctl.
// Useful for tests and for callers that ship a vendored eksctl binary.
func WithBinary(binary string) Option {
	return func(c *Client) {
		if binary != "" {
			c.binary = binary
		}
	}
}

// WithRunner overrides the Runner used to execute eksctl commands.
// Primarily useful for testing without the eksctl binary on PATH.
func WithRunner(runner Runner) Option {
	return func(c *Client) {
		if runner != nil {
			c.runner = runner
		}
	}
}

// NewClient returns a Client using the eksctl binary on PATH and the default
// ExecRunner. Use the Options to override either.
func NewClient(opts ...Option) *Client {
	client := &Client{
		binary: DefaultBinary,
		runner: ExecRunner{},
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// Binary returns the binary name this client will invoke.
func (c *Client) Binary() string {
	return c.binary
}

// CheckAvailable verifies the eksctl binary can be found and is executable.
// When a custom binary path was supplied via WithBinary, the check uses
// exec.LookPath on that exact value (which also resolves relative paths).
//
// This method is intentionally side-effect free: it does not call `eksctl
// version` because doing so on every `ksail cluster create` would add
// hundreds of milliseconds of latency.
func (c *Client) CheckAvailable() error {
	_, err := exec.LookPath(c.binary)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBinaryNotFound, err)
	}

	return nil
}

// Exec runs the eksctl binary with the given arguments. It is the low-level
// escape hatch used by all higher-level methods on this client and can be
// used directly when a helper has not been written yet.
func (c *Client) Exec(ctx context.Context, args ...string) ([]byte, []byte, error) {
	stdout, stderr, err := c.runner.Run(ctx, c.binary, args, nil)
	if err != nil {
		return stdout, stderr, wrapExecErr(args, stderr, err)
	}

	return stdout, stderr, nil
}

// ExecWithStdin runs eksctl with the given arguments and feeds stdin from the
// provided reader. Useful for `eksctl create cluster -f -` style invocations.
func (c *Client) ExecWithStdin(
	ctx context.Context,
	stdin io.Reader,
	args ...string,
) ([]byte, []byte, error) {
	stdout, stderr, err := c.runner.Run(ctx, c.binary, args, stdin)
	if err != nil {
		return stdout, stderr, wrapExecErr(args, stderr, err)
	}

	return stdout, stderr, nil
}

// wrapExecErr annotates an exec failure with the invoked arguments and the
// first line of stderr (if any) to produce actionable error messages without
// leaking the full eksctl output into Go error strings.
func wrapExecErr(args []string, stderr []byte, err error) error {
	const (
		firstLineParts = 2
	)

	firstStderrLine := strings.SplitN(strings.TrimSpace(string(stderr)), "\n", firstLineParts)[0]
	if firstStderrLine == "" {
		return fmt.Errorf("eksctl %s: %w", strings.Join(args, " "), err)
	}

	return fmt.Errorf("eksctl %s: %w: %s", strings.Join(args, " "), err, firstStderrLine)
}
