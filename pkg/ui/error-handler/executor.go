package errorhandler

import (
	"bytes"
	"strings"

	"github.com/spf13/cobra"
)

// Executor type.

// Executor coordinates Cobra execution, capturing stderr output and surfacing aggregated errors.
type Executor struct {
	normalizer DefaultNormalizer
}

// NewExecutor constructs an Executor.
func NewExecutor() *Executor {
	return &Executor{normalizer: DefaultNormalizer{}}
}

// Execute runs the provided command while intercepting Cobra's error stream.
// It returns nil on success, or a *CommandError containing both the normalized message
// and the original error to preserve error-chain semantics.
//
// The function captures stderr output during command execution and applies
// normalization to produce user-friendly error messages.
func (e *Executor) Execute(cmd *cobra.Command) error {
	if cmd == nil {
		return nil
	}

	var errBuf bytes.Buffer

	originalErrWriter := cmd.ErrOrStderr()

	cmd.SetErr(&errBuf)
	defer cmd.SetErr(originalErrWriter)

	err := cmd.Execute()
	if err == nil {
		return nil
	}

	message := e.normalizer.Normalize(errBuf.String())

	return &CommandError{
		message: message,
		cause:   err,
	}
}

// CommandError type.

// CommandError represents a Cobra execution failure augmented with normalized stderr output.
type CommandError struct {
	message string
	cause   error
}

// Error implements the error interface.
func (e *CommandError) Error() string {
	switch {
	case e == nil:
		return ""
	case e.cause == nil:
		return e.message
	case e.message != "":
		if strings.Contains(e.message, e.cause.Error()) {
			return e.message
		}

		return e.message + ": " + e.cause.Error()
	default:
		return e.cause.Error()
	}
}

// Unwrap exposes the underlying cause for errors.Is/errors.As consumers.
func (e *CommandError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.cause
}

// DefaultNormalizer implementation.

// DefaultNormalizer implements Normalizer with the same semantics previously embedded in root.go.
type DefaultNormalizer struct{}

// Normalize trims whitespace, removes redundant "Error:" prefixes, and preserves multi-line usage hints.
func (DefaultNormalizer) Normalize(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return ""
	}

	first := strings.TrimSpace(lines[0])
	first = strings.TrimPrefix(first, "Error: ")
	lines[0] = first

	return strings.Join(lines, "\n")
}
