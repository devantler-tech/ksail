package errorhandler_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/errorhandler"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

var (
	errTestBoom = errors.New("boom")
	errWrapped  = errors.New("wrapped")
)

func TestExecutorExecuteSuccess(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use: "test",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}

	executor := errorhandler.NewExecutor()

	err := executor.Execute(cmd)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// TestExecutorExecuteDeliversWarningsOnSuccess guards against silently swallowing
// warnings: a command that SUCCEEDS while writing a warning to stderr must have
// that warning reach the real stderr.
func TestExecutorExecuteDeliversWarningsOnSuccess(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	cmd := &cobra.Command{
		Use: "test",
		RunE: func(c *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(c.ErrOrStderr(), "heads up: incomplete values")

			return nil
		},
	}
	cmd.SetErr(&stderr)

	executor := errorhandler.NewExecutor()

	err := executor.Execute(cmd)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got := stderr.String(); !strings.Contains(got, "heads up: incomplete values") {
		t.Fatalf("expected the success-path warning to reach stderr, got %q", got)
	}
}

// exitCodeError is a test error carrying a custom KSail exit code, mirroring
// DriftExitError: a valid, non-failing outcome rather than a command failure.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

func (e *exitCodeError) KSailExitCode() int { return e.code }

// TestExecutorExecuteDeliversWarningsOnExitCodeResult guards the custom-exit-code path
// (e.g. `cluster diff --exit-code` detecting drift): main.go surfaces such results as a
// process exit code and prints no error, so a warning the run wrote to stderr must still
// reach the real stderr. It also asserts the custom exit code stays detectable via
// errors.As so main.go keeps propagating it.
func TestExecutorExecuteDeliversWarningsOnExitCodeResult(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	cmd := &cobra.Command{
		Use:           "test",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(c *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(c.ErrOrStderr(), "heads up: drift detected")

			return &exitCodeError{code: 2}
		},
	}
	cmd.SetErr(&stderr)

	executor := errorhandler.NewExecutor()

	err := executor.Execute(cmd)
	if err == nil {
		t.Fatal("expected the exit-code error to propagate, got nil")
	}

	var coder interface{ KSailExitCode() int }
	if !errors.As(err, &coder) {
		t.Fatalf("expected the custom exit code to remain detectable via errors.As, got %T", err)
	}

	if got := stderr.String(); !strings.Contains(got, "heads up: drift detected") {
		t.Fatalf("expected the exit-code-path warning to reach stderr, got %q", got)
	}
}

// TestExecutorExecuteDropsCobraErrorOnExitCodeResult guards that the exit-code path does
// NOT print Cobra's automatic "Error: <err>" line for a command that leaves SilenceErrors
// unset (like `cluster diff`, which sets only SilenceUsage). main.go prints no error for
// an exit-code result, so that line would be spurious noise. The command's own warning
// must still reach stderr.
func TestExecutorExecuteDropsCobraErrorOnExitCodeResult(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	cmd := &cobra.Command{
		Use: "test",
		// Mirror `cluster diff`: SilenceUsage set, SilenceErrors UNSET.
		SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(c.ErrOrStderr(), "heads up: drift detected")

			return &exitCodeError{code: 2}
		},
	}
	cmd.SetErr(&stderr)

	executor := errorhandler.NewExecutor()

	err := executor.Execute(cmd)
	if err == nil {
		t.Fatal("expected the exit-code error to propagate, got nil")
	}

	got := stderr.String()
	if !strings.Contains(got, "heads up: drift detected") {
		t.Fatalf("expected the exit-code-path warning to reach stderr, got %q", got)
	}

	if strings.Contains(got, "Error: exit code 2") {
		t.Fatalf("Cobra's auto error line must not be printed for exit-code results, got %q", got)
	}
}

func TestExecutorExecuteNilCommand(t *testing.T) {
	t.Parallel()

	executor := errorhandler.NewExecutor()

	err := executor.Execute(nil)
	if err != nil {
		t.Fatalf("expected nil command to succeed, got %v", err)
	}
}

func TestExecutorExecuteInvalidSubcommand(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "test"}
	root.AddCommand(&cobra.Command{Use: "valid"})
	root.SetArgs([]string{"invalid"})

	executor := errorhandler.NewExecutor()

	err := executor.Execute(root)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	snaps.MatchSnapshot(t, err.Error())
}

func TestCommandErrorErrorNilReceiver(t *testing.T) {
	t.Parallel()

	actual := commandErrorString(nil)
	if actual != "" {
		t.Fatalf("expected empty string, got %q", actual)
	}
}

func TestCommandErrorErrorEmptyStruct(t *testing.T) {
	t.Parallel()

	actual := commandErrorString(&errorhandler.CommandError{})
	if actual != "" {
		t.Fatalf("expected empty string, got %q", actual)
	}
}

func TestCommandErrorErrorCauseOnlyWhenMessageEmpty(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use: "test",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errTestBoom
		},
	}

	err := executeAndRequireCommandError(t, cmd)
	snaps.MatchSnapshot(t, err.Error())
}

func TestCommandErrorUnwrap(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use: "test",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errWrapped
		},
	}

	executor := errorhandler.NewExecutor()

	err := executor.Execute(cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, errWrapped) {
		t.Fatalf("expected errors.Is to match original cause")
	}

	if (*errorhandler.CommandError)(nil).Unwrap() != nil {
		t.Fatalf("expected nil receiver unwrap to return nil")
	}
}

func executeAndRequireCommandError(t *testing.T, cmd *cobra.Command) *errorhandler.CommandError {
	t.Helper()

	executor := errorhandler.NewExecutor()

	err := executor.Execute(cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cmdErr *errorhandler.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("expected error to be *CommandError, got %T (%v)", err, err)
	}

	return cmdErr
}

func commandErrorString(err *errorhandler.CommandError) string {
	if err == nil {
		var cmdErr *errorhandler.CommandError

		return cmdErr.Error()
	}

	return err.Error()
}

// warningWithAddress quotes a documentation-range address (RFC 5737), which the
// address mask treats exactly like a public server address.
const warningWithAddress = "heads up: 203.0.113.10:6443 answered slowly"

func warningCommand(result error) *cobra.Command {
	return &cobra.Command{
		Use:           "test",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(c *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(c.ErrOrStderr(), warningWithAddress)

			return result
		},
	}
}

// requireWarningWithoutAddress runs a command that writes a warning and ends with
// result, and requires the warning to hide its address. A warning reaches the same
// public logs a failed command's error reaches.
func requireWarningWithoutAddress(t *testing.T, result error) {
	t.Helper()

	var stderr bytes.Buffer

	cmd := warningCommand(result)
	cmd.SetErr(&stderr)

	_ = errorhandler.NewExecutor().Execute(cmd)

	got := stderr.String()
	if !strings.Contains(got, "heads up: <address hidden>:6443 answered slowly") {
		t.Fatalf("expected the warning with its address hidden, got %q", got)
	}

	if strings.Contains(got, "203.0.113.10") {
		t.Fatalf("the warning still names the address: %q", got)
	}
}

func TestExecutorExecuteHidesAddressesInWarningsOnSuccess(t *testing.T) {
	t.Setenv("KSAIL_SHOW_ADDRESSES", "")

	requireWarningWithoutAddress(t, nil)
}

func TestExecutorExecuteHidesAddressesInWarningsOnExitCode(t *testing.T) {
	t.Setenv("KSAIL_SHOW_ADDRESSES", "")

	requireWarningWithoutAddress(t, &exitCodeError{code: 2})
}

func TestExecutorExecuteShowsAddressesInWarningsWhenOptedIn(t *testing.T) {
	t.Setenv("KSAIL_SHOW_ADDRESSES", "true")

	var stderr bytes.Buffer

	cmd := warningCommand(nil)
	cmd.SetErr(&stderr)

	_ = errorhandler.NewExecutor().Execute(cmd)

	if got := stderr.String(); !strings.Contains(got, warningWithAddress) {
		t.Fatalf("expected the warning unchanged, got %q", got)
	}
}

// TestExecutorExecuteStreamsStderrWhileCommandRuns guards the reason stderr is not
// buffered: a long-running command's notice must reach the real stderr while the
// command is still running, not after it returns.
func TestExecutorExecuteStreamsStderrWhileCommandRuns(t *testing.T) {
	t.Setenv("KSAIL_SHOW_ADDRESSES", "")

	var (
		stderr     bytes.Buffer
		seenInside string
	)

	cmd := &cobra.Command{
		Use: "test",
		RunE: func(c *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(c.ErrOrStderr(), warningWithAddress)
			seenInside = stderr.String()

			return nil
		},
	}
	cmd.SetErr(&stderr)

	err := errorhandler.NewExecutor().Execute(cmd)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(seenInside, "heads up: <address hidden>:6443 answered slowly") {
		t.Fatalf("expected the notice on stderr while the command ran, got %q", seenInside)
	}
}

// TestExecutorExecuteFailurePrintsNoErrorOfItsOwn guards against printing a failure
// twice: the command's warning reaches stderr live, Cobra's error line does not, and
// the returned error holds only the failure, which the entrypoint prints once.
func TestExecutorExecuteFailurePrintsNoErrorOfItsOwn(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	cmd := &cobra.Command{
		Use:          "test",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(c.ErrOrStderr(), "heads up: incomplete values")

			return errTestBoom
		},
	}
	cmd.SetErr(&stderr)

	cmdErr := executeAndRequireCommandError(t, cmd)

	if got := stderr.String(); got != "heads up: incomplete values\n" {
		t.Fatalf("expected only the warning on stderr, got %q", got)
	}

	if got := cmdErr.Error(); got != errTestBoom.Error() {
		t.Fatalf("expected the error to hold only the failure, got %q", got)
	}
}

// TestExecutorExecuteFlushesPartialLineBeforeReturning guards the order of a failure's
// output: the end of a partial line must be out before the caller prints the error,
// not left to the masking writer's timer.
func TestExecutorExecuteFlushesPartialLineBeforeReturning(t *testing.T) {
	t.Setenv("KSAIL_SHOW_ADDRESSES", "")

	var stderr bytes.Buffer

	cmd := &cobra.Command{
		Use:          "test",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error {
			_, _ = fmt.Fprint(c.ErrOrStderr(), "reached 203.0.113.10")

			return errTestBoom
		},
	}
	cmd.SetErr(&stderr)

	_ = errorhandler.NewExecutor().Execute(cmd)

	if got := stderr.String(); got != "reached <address hidden>" {
		t.Fatalf("expected the partial line flushed and masked, got %q", got)
	}
}

// TestExecutorExecuteRestoresCommand guards that the executor leaves the command as it
// found it, because the entrypoint prints the failure through the command's writer.
func TestExecutorExecuteRestoresCommand(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	cmd := &cobra.Command{
		Use: "test",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	cmd.SetErr(&stderr)

	err := errorhandler.NewExecutor().Execute(cmd)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cmd.SilenceErrors {
		t.Fatal("expected SilenceErrors restored")
	}

	if cmd.ErrOrStderr() != &stderr {
		t.Fatal("expected the original error writer restored")
	}
}
