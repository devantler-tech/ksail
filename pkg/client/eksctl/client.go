package eksctl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
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

// EnvironmentRunner is the optional extension implemented by runners that can
// execute with an explicit child-process environment. It keeps the original
// Runner interface source-compatible while allowing credential-isolated EKS
// clients to fail closed when an injected runner cannot honor their mapping.
type EnvironmentRunner interface {
	RunWithEnvironment(
		ctx context.Context,
		name string,
		args []string,
		stdin io.Reader,
		environment []string,
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
	return ExecRunner{}.RunWithEnvironment(ctx, name, args, stdin, nil)
}

// RunWithEnvironment executes the command with environment. A nil environment
// preserves os/exec's default inheritance; a non-nil slice is used verbatim.
func (ExecRunner) RunWithEnvironment(
	ctx context.Context,
	name string,
	args []string,
	stdin io.Reader,
	environment []string,
) ([]byte, []byte, error) {
	// #nosec G204 -- This uses os/exec directly with a program name and argv
	// slice; it does not invoke a shell, so user-influenced values in args
	// (cluster name, region, config file paths from ksail.yaml) are passed
	// as literal arguments rather than shell-interpreted command text.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = environment

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
	binary      string
	runner      Runner
	environment []string

	requireCredentialValues bool
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

// WithEnvironment sets the complete environment passed to every eksctl child
// process. The slice is cloned at construction and again per invocation so
// callers, concurrent commands, and injected runners cannot mutate each other.
func WithEnvironment(environment []string) Option {
	return func(c *Client) {
		c.environment = cloneStrings(environment)
	}
}

// RequireCredentialValues makes execution fail closed unless the explicit
// child environment contains either a profile or a complete static credential
// pair. Use it when custom source names were configured so an unset alias
// cannot silently fall back to another ambient identity.
func RequireCredentialValues() Option {
	return func(c *Client) {
		c.requireCredentialValues = true
	}
}

// NewClient returns a Client using the eksctl binary on PATH and the default
// ExecRunner. Use the Options to override either.
func NewClient(opts ...Option) *Client {
	client := &Client{
		binary:                  DefaultBinary,
		runner:                  ExecRunner{},
		environment:             nil,
		requireCredentialValues: false,
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
	stdout, stderr, err := c.run(ctx, args, nil)

	stderr = c.redactCredentialValues(stderr)
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
	stdout, stderr, err := c.run(ctx, args, stdin)

	stderr = c.redactCredentialValues(stderr)
	if err != nil {
		return stdout, stderr, wrapExecErr(args, stderr, err)
	}

	return stdout, stderr, nil
}

func (c *Client) run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
) ([]byte, []byte, error) {
	err := c.validateCredentialValues()
	if err != nil {
		return nil, nil, err
	}

	if c.environment == nil {
		stdout, stderr, err := c.runner.Run(ctx, c.binary, args, stdin)
		if err != nil {
			return stdout, stderr, fmt.Errorf("run eksctl: %w", err)
		}

		return stdout, stderr, nil
	}

	environmentRunner, ok := c.runner.(EnvironmentRunner)
	if !ok {
		return nil, nil, ErrRunnerEnvironmentUnsupported
	}

	stdout, stderr, err := environmentRunner.RunWithEnvironment(
		ctx,
		c.binary,
		args,
		stdin,
		cloneStrings(c.environment),
	)
	if err != nil {
		return stdout, stderr, fmt.Errorf("run eksctl with explicit environment: %w", err)
	}

	return stdout, stderr, nil
}

func (c *Client) validateCredentialValues() error {
	if !c.requireCredentialValues {
		return nil
	}

	values := environmentValues(c.environment)
	profile := values["AWS_PROFILE"]
	accessKeyID := values["AWS_ACCESS_KEY_ID"]
	secretAccessKey := values["AWS_SECRET_ACCESS_KEY"]
	sessionToken := values["AWS_SESSION_TOKEN"]
	hasAccessKey := accessKeyID != ""
	hasSecretKey := secretAccessKey != ""

	if hasAccessKey != hasSecretKey || (sessionToken != "" && !hasAccessKey) {
		return ErrIncompleteStaticCredentials
	}

	if profile == "" && !hasAccessKey {
		return ErrExplicitCredentialsUnavailable
	}

	return nil
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	return append([]string{}, values...)
}

func (c *Client) redactCredentialValues(stderr []byte) []byte {
	if len(stderr) == 0 || len(c.environment) == 0 {
		return stderr
	}

	uniqueValues := make(map[string]struct{})

	for _, entry := range c.environment {
		name, value, found := strings.Cut(entry, "=")
		if !found || value == "" {
			continue
		}

		switch name {
		case "AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN":
			uniqueValues[value] = struct{}{}
		}
	}

	values := make([]string, 0, len(uniqueValues))
	for value := range uniqueValues {
		values = append(values, value)
	}

	sort.Slice(values, func(i, j int) bool {
		return len(values[i]) > len(values[j])
	})

	redacted := string(stderr)
	for _, value := range values {
		redacted = strings.ReplaceAll(redacted, value, "[REDACTED]")
	}

	return []byte(redacted)
}

func environmentValues(environment []string) map[string]string {
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = value
		}
	}

	return values
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
