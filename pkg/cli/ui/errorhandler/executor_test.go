package errorhandler_test

import (
	"errors"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/errorhandler"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
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

func TestDefaultNormalizerNormalize(t *testing.T) {
	t.Parallel()

	normalizer := errorhandler.DefaultNormalizer{}

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

			actual := normalizer.Normalize(testCase.input)
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
