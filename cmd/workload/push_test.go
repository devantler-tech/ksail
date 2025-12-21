package workload_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/cmd/workload"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
)

func TestNewPushCmdHasValidateFlag(t *testing.T) {
	t.Parallel()

	cmd := workload.NewPushCmd(runtime.New(nil))

	// Check if --validate flag exists
	validateFlag := cmd.Flags().Lookup("validate")
	if validateFlag == nil {
		t.Fatal("expected --validate flag to exist")
	}

	// Check default value
	if validateFlag.DefValue != "false" {
		t.Fatalf("expected --validate flag default value to be false, got %s", validateFlag.DefValue)
	}

	// Check usage text
	expectedUsage := "Validate manifests after pushing"
	if validateFlag.Usage != expectedUsage {
		t.Fatalf("expected --validate flag usage to be %q, got %q", expectedUsage, validateFlag.Usage)
	}
}

func TestPushCmdShowsValidateFlagInHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewPushCmd(runtime.New(nil))

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error executing push --help, got %v", err)
	}

	helpText := output.String()

	// Check that --validate flag is documented in help
	if !strings.Contains(helpText, "--validate") {
		t.Fatal("expected help text to include --validate flag")
	}

	if !strings.Contains(helpText, "Validate manifests after pushing") {
		t.Fatal("expected help text to include validate flag description")
	}
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := workload.NewPushCmd(runtime.New(nil))
			cmd.SetArgs(tc.args)

			// Parse flags without executing the command
			err := cmd.ParseFlags(tc.args)
			if err != nil {
				t.Fatalf("expected no error parsing flags, got %v", err)
			}

			validate, err := cmd.Flags().GetBool("validate")
			if err != nil {
				t.Fatalf("expected no error getting validate flag, got %v", err)
			}

			if validate != tc.expected {
				t.Fatalf("expected validate flag to be %v, got %v", tc.expected, validate)
			}
		})
	}
}
