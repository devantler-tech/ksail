//nolint:testpackage // Testing internal functions requires same package
package cluster

import (
	"bytes"
	"testing"

	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
)

func TestNewUpdateCmd(t *testing.T) {
	t.Parallel()

	runtimeContainer := &runtime.Runtime{}
	cmd := NewUpdateCmd(runtimeContainer)

	// Verify command basics
	if cmd.Use != "update" {
		t.Errorf("expected Use to be 'update', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}

	if cmd.Long == "" {
		t.Error("expected Long description to be set")
	}

	// Verify flags
	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Error("expected --force flag to exist")
	}

	nameFlag := cmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Error("expected --name flag to exist")
	}

	mirrorRegistryFlag := cmd.Flags().Lookup("mirror-registry")
	if mirrorRegistryFlag == nil {
		t.Error("expected --mirror-registry flag to exist")
	}

	dryRunFlag := cmd.Flags().Lookup("dry-run")
	if dryRunFlag == nil {
		t.Error("expected --dry-run flag to exist")
	}
}

func TestPromptForUpdateConfirmation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "user confirms with 'yes'",
			input:    "yes\n",
			expected: true,
		},
		{
			name:     "user confirms with 'YES'",
			input:    "YES\n",
			expected: true,
		},
		{
			name:     "user confirms with 'Yes'",
			input:    "Yes\n",
			expected: true,
		},
		{
			name:     "user rejects with 'no'",
			input:    "no\n",
			expected: false,
		},
		{
			name:     "user rejects with empty input",
			input:    "\n",
			expected: false,
		},
		{
			name:     "user rejects with random text",
			input:    "maybe\n",
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtimeContainer := &runtime.Runtime{}
			cmd := NewUpdateCmd(runtimeContainer)

			// Set up input/output buffers
			inputBuf := bytes.NewBufferString(testCase.input)
			outputBuf := &bytes.Buffer{}

			cmd.SetIn(inputBuf)
			cmd.SetOut(outputBuf)
			cmd.SetErr(outputBuf)

			// Test prompt function
			result := promptForUpdateConfirmation(cmd, "test-cluster")

			if result != testCase.expected {
				t.Errorf("expected %v, got %v", testCase.expected, result)
			}
		})
	}
}
