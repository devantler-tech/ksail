package errorhandler

import (
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/addressmask"
	"github.com/spf13/cobra"
)

// unknownCommandPrefix starts the error Cobra returns for an unknown command.
// Cobra adds its usage hint to the error line it prints, not to the error, so the hint
// has to be restored here now that the line is silenced.
const unknownCommandPrefix = "unknown command "

// Executor type.

// Executor coordinates Cobra execution, streaming stderr live and returning the failure as a CommandError.
type Executor struct{}

// NewExecutor constructs an Executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// Execute runs the provided command. It returns nil on success, or a *CommandError
// carrying the original error to preserve error-chain semantics.
//
// Anything the command writes to stderr reaches the real stderr while the command
// runs, on every exit path, so a long-running command's notices are seen in time.
// Publicly routable addresses are masked on the way, because stderr reaches the same
// public logs as a failed command's error; the operator opts back in through
// addressmask.ShowAddressesEnvVar.
//
// Cobra's own "Error: <err>" line is silenced, because the returned error carries the
// same text and the entrypoint prints it once. A custom exit-code result (e.g.
// DriftExitError from `cluster diff --exit-code`) prints no error at all.
func (e *Executor) Execute(cmd *cobra.Command) error {
	if cmd == nil {
		return nil
	}

	originalErrWriter := cmd.ErrOrStderr()
	liveErrWriter := addressmask.New().Writer(originalErrWriter)

	cmd.SetErr(liveErrWriter)
	defer cmd.SetErr(originalErrWriter)

	root := cmd.Root()
	silenceErrors := root.SilenceErrors
	root.SilenceErrors = true

	defer func() { root.SilenceErrors = silenceErrors }()

	executed, err := cmd.ExecuteC()

	// The end of a last partial line must be out before the caller prints the error.
	addressmask.Flush(liveErrWriter)

	if err == nil {
		return nil
	}

	if !silenceErrors && strings.HasPrefix(err.Error(), unknownCommandPrefix) {
		return &CommandError{
			cause: err,
			hint:  "Run '" + executed.CommandPath() + " --help' for usage.",
		}
	}

	return &CommandError{cause: err}
}

// CommandError type.

// CommandError represents a Cobra execution failure.
type CommandError struct {
	cause error
	hint  string
}

// Error implements the error interface.
func (e *CommandError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}

	if e.hint != "" {
		return e.cause.Error() + "\n" + e.hint
	}

	return e.cause.Error()
}

// Unwrap exposes the underlying cause for errors.Is/errors.As consumers.
func (e *CommandError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.cause
}
