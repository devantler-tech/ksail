package errorhandler

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// Executor type.

// Executor coordinates Cobra execution, capturing stderr output and surfacing aggregated errors.
type Executor struct{}

// NewExecutor constructs an Executor.
func NewExecutor() *Executor {
	return &Executor{}
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
		flushWarnings(originalErrWriter, &errBuf)

		return nil
	}

	// A custom exit-code result (e.g. DriftExitError from `cluster diff --exit-code`)
	// is a valid, non-failing outcome: main.go surfaces it as a process exit code and
	// prints no error, so any warnings the run captured would otherwise be lost. Flush
	// them like the success path. Genuine failures instead surface their stderr via the
	// normalized message that main.go prints, so they are not flushed here.
	if isExitCodeResult(err) {
		flushWarnings(originalErrWriter, &errBuf)
	}

	message := normalize(errBuf.String())

	return &CommandError{
		message: message,
		cause:   err,
	}
}

// flushWarnings forwards any stderr a non-failing run captured to the real writer, so
// warnings and notices reach the user. The capture exists only to normalize a failing
// command's stderr into the returned error; an empty buffer is a no-op, so commands
// that write nothing are unaffected.
func flushWarnings(w io.Writer, buf *bytes.Buffer) {
	if buf.Len() > 0 {
		_, _ = w.Write(buf.Bytes())
	}
}

// isExitCodeResult reports whether err carries a custom KSail exit code — a valid,
// non-failing outcome (e.g. drift detected) rather than a command failure. It mirrors
// the structural interface main.go uses to translate such errors into process exit
// codes, without coupling this package to specific command types.
func isExitCodeResult(err error) bool {
	var exitCoder interface{ KSailExitCode() int }

	return errors.As(err, &exitCoder)
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

// normalize trims whitespace, removes redundant "Error:" prefixes, and preserves multi-line usage hints.
func normalize(raw string) string {
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
