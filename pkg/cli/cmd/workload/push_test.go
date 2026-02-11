package workload_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
)

func TestNewPushCmdHasValidateFlag(t *testing.T) {
	t.Parallel()

	cmd := workload.NewPushCmd(di.New(nil))

	// Check if --validate flag exists
	validateFlag := cmd.Flags().Lookup("validate")
	if validateFlag == nil {
		t.Fatal("expected --validate flag to exist")
	}

	// Check default value
	if validateFlag.DefValue != "false" {
		t.Fatalf(
			"expected --validate flag default value to be false, got %s",
			validateFlag.DefValue,
		)
	}

	// Check usage text
	expectedUsage := "Validate manifests before pushing"
	if validateFlag.Usage != expectedUsage {
		t.Fatalf(
			"expected --validate flag usage to be %q, got %q",
			expectedUsage,
			validateFlag.Usage,
		)
	}
}

func TestPushCmdShowsValidateFlagInHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewPushCmd(di.New(nil))

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error executing push --help, got %v", err)
	}

	snaps.MatchSnapshot(t, output.String())
}

func TestPushCmdAcceptsValidateFlag(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "validate flag not set",
			args:     []string{},
			expected: false,
		},
		{
			name:     "validate flag set to true",
			args:     []string{"--validate=true"},
			expected: true,
		},
		{
			name:     "validate flag set to false",
			args:     []string{"--validate=false"},
			expected: false,
		},
		{
			name:     "validate flag shorthand",
			args:     []string{"--validate"},
			expected: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := workload.NewPushCmd(di.New(nil))
			cmd.SetArgs(testCase.args)

			// Parse flags without executing the command
			err := cmd.ParseFlags(testCase.args)
			if err != nil {
				t.Fatalf("expected no error parsing flags, got %v", err)
			}

			validate, err := cmd.Flags().GetBool("validate")
			if err != nil {
				t.Fatalf("expected no error getting validate flag, got %v", err)
			}

			if validate != testCase.expected {
				t.Fatalf("expected validate flag to be %v, got %v", testCase.expected, validate)
			}
		})
	}
}
