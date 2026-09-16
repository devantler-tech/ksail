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

// ProgressRunner is the optional extension implemented by runners that can write
// a command's output to progress while it runs, in addition to returning it
// buffered. A nil environment preserves os/exec's default inheritance.
type ProgressRunner interface {
	RunWithProgress(
		ctx context.Context,
		name string,
		args []string,
		stdin io.Reader,
		environment []string,
		progress io.Writer,
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
	return runCommand(ctx, name, args, stdin, environment, nil)
}

// RunWithProgress executes the command with environment, writing each complete
// output line to progress as it arrives while still returning both buffers.
func (ExecRunner) RunWithProgress(
	ctx context.Context,
	name string,
	args []string,
	stdin io.Reader,
	environment []string,
	progress io.Writer,
) ([]byte, []byte, error) {
	return runCommand(ctx, name, args, stdin, environment, progress)
}

// runCommand executes one eksctl process, optionally mirroring its output to progress.
func runCommand(
	ctx context.Context,
	name string,
	args []string,
	stdin io.Reader,
	environment []string,
	progress io.Writer,
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

	if progress != nil {
		// One line buffer per stream, so stdout and stderr never interleave mid-line. os/exec copies
		// the two streams on separate goroutines, and a caller's writer need not be safe for
		// concurrent use, so both buffers write through one shared lock.
		shared := newLockedWriter(progress)
		stdoutLines := newLineWriter(shared, nil)
		stderrLines := newLineWriter(shared, nil)

		defer stdoutLines.Flush()
		defer stderrLines.Flush()

		cmd.Stdout = io.MultiWriter(&stdout, stdoutLines)
		cmd.Stderr = io.MultiWriter(&stderr, stderrLines)
	}

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
	progress    io.Writer

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

// WithProgressWriter streams the output of long-running eksctl commands (create,
// delete, scale, upgrade) to w while they run, with credential values redacted.
// Read-only listings are never streamed, because their stdout is parsed. A nil
// writer disables streaming.
func WithProgressWriter(w io.Writer) Option {
	return func(c *Client) {
		c.progress = w
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
	return c.exec(ctx, nil, nil, args)
}

// ExecWithStdin runs eksctl with the given arguments and feeds stdin from the
// provided reader. Useful for `eksctl create cluster -f -` style invocations.
func (c *Client) ExecWithStdin(
	ctx context.Context,
	stdin io.Reader,
	args ...string,
) ([]byte, []byte, error) {
	return c.exec(ctx, stdin, nil, args)
}

// execWithProgress runs a long-running mutating command, streaming its output to the
// configured progress writer. Commands whose stdout is parsed must use Exec instead.
func (c *Client) execWithProgress(ctx context.Context, args ...string) error {
	_, _, err := c.exec(ctx, nil, c.progress, args)

	return err
}

// exec runs eksctl, redacts credential values from stderr, and wraps a failure with the
// trailing output of both streams.
func (c *Client) exec(
	ctx context.Context,
	stdin io.Reader,
	progress io.Writer,
	args []string,
) ([]byte, []byte, error) {
	stdout, stderr, err := c.run(ctx, args, stdin, progress)

	stderr = c.redactCredentialValues(stderr)
	if err != nil {
		return stdout, stderr, wrapExecErr(args, c.redactCredentialValues(stdout), stderr, err)
	}

	return stdout, stderr, nil
}

// run validates the credential boundary before invoking the runner with a defensive environment snapshot.
func (c *Client) run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	progress io.Writer,
) ([]byte, []byte, error) {
	err := c.validateCredentialValues()
	if err != nil {
		return nil, nil, err
	}

	if progressRunner, ok := c.runner.(ProgressRunner); ok && progress != nil {
		lines := newLineWriter(progress, c.redactCredentialValues)
		defer lines.Flush()

		stdout, stderr, err := progressRunner.RunWithProgress(
			ctx,
			c.binary,
			args,
			stdin,
			cloneStrings(c.environment),
			lines,
		)
		if err != nil {
			return stdout, stderr, fmt.Errorf("run eksctl with progress: %w", err)
		}

		return stdout, stderr, nil
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

// validateCredentialValues rejects incomplete or required-but-empty explicit selections before process execution.
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

// cloneStrings returns a defensive copy while preserving nil as the parent-environment inheritance sentinel.
func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	return append([]string{}, values...)
}

// redactCredentialValues removes resolved secret values from stderr before it reaches callers or wrapped errors.
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
		case "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN":
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

// environmentValues parses child-environment entries with the last value for each name taking precedence.
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

// wrapExecErr annotates an exec failure with the invoked arguments, the first
// line of stderr (if any), and a bounded tail of stdout and stderr. eksctl logs
// the cause of a failure on stdout and prints only a generic line on stderr, so
// the tail is what makes the error actionable. Both streams must already be
// redacted; the tail is capped in both lines and bytes, so neither a long output nor a single
// very long line can carry the whole of it into an error.
func wrapExecErr(args []string, stdout, stderr []byte, err error) error {
	const (
		firstLineParts = 2
	)

	firstStderrLine := strings.SplitN(strings.TrimSpace(string(stderr)), "\n", firstLineParts)[0]

	wrapped := fmt.Errorf("eksctl %s: %w", strings.Join(args, " "), err)
	if firstStderrLine != "" {
		wrapped = fmt.Errorf("eksctl %s: %w: %s", strings.Join(args, " "), err, firstStderrLine)
	}

	return withOutputTail(wrapped, stdout, stderr, firstStderrLine)
}
