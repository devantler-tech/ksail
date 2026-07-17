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
	errTestBoom        = errors.New("boom")
	errOriginalFailure = errors.New("original failure")
	errBoomOriginal    = errors.New("boom: original failure")
	errWrapped         = errors.New("wrapped")
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

// TestExecutorExecuteFlushesWarningsOnSuccess guards against silently swallowing
// warnings: the executor captures stderr to normalize a failing command's error,
// but a command that SUCCEEDS while writing a warning to stderr must still have
// that warning reach the real stderr — a regression guard for warnings being
// discarded on the success path.
func TestExecutorExecuteFlushesWarningsOnSuccess(t *testing.T) {
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

func TestCommandErrorErrorMessageAndCauseConcatenatedWhenDistinct(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use:           "test",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.PrintErrln("normalized")

			return errOriginalFailure
		},
	}

	err := executeAndRequireCommandError(t, cmd)
	snaps.MatchSnapshot(t, err.Error())
}

func TestCommandErrorErrorMessageRetainedWhenAlreadyIncludesCause(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use:           "test",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.PrintErrln("boom: original failure")

			return errBoomOriginal
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

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input returns empty string",
			input:    "   \n\t  ",
			expected: "",
		},
		{
			name:     "strips error prefix and trims",
			input:    "  Error: something bad \nRun help\n",
			expected: "something bad\nRun help",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual := errorhandler.NormalizeForTest(testCase.input)
			if actual != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, actual)
			}
		})
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
