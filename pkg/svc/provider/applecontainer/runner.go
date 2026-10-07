package applecontainer

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DefaultBinary is the name of Apple's container CLI.
const DefaultBinary = "container"

// Runner runs the `container` CLI with the given arguments and returns its standard output.
// It is the seam that lets the provider be tested without the runtime installed.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// ExecRunner is a Runner that executes the CLI as a child process.
type ExecRunner struct {
	binary string
}

// NewExecRunner creates a Runner for the given CLI binary name or path.
// An empty binary selects DefaultBinary.
func NewExecRunner(binary string) *ExecRunner {
	if binary == "" {
		binary = DefaultBinary
	}

	return &ExecRunner{binary: binary}
}

// Run executes the CLI. A missing binary yields ErrCLINotFound; a failing invocation yields
// ErrCommandFailed carrying the subcommand and the CLI's own error text, but never the full
// argument list, which can hold environment values.
func (r *ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	path, err := exec.LookPath(r.binary)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCLINotFound, err)
	}

	var stdout, stderr bytes.Buffer

	// The binary is a fixed tool name and every argument is passed as its own argv entry, so no
	// shell interprets them.
	cmd := exec.CommandContext(ctx, path, args...) // #nosec G204
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %s: %w: %s",
			ErrCommandFailed, subcommand(args), err, strings.TrimSpace(stderr.String()),
		)
	}

	return stdout.Bytes(), nil
}

// subcommand returns the leading non-flag arguments (for example "volume list") for error text.
func subcommand(args []string) string {
	words := make([]string, 0, len(args))

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}

		words = append(words, arg)
	}

	return strings.Join(words, " ")
}
