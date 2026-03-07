package cluster_test

import (
	"strings"
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v5/pkg/di"
)

func TestNewUpdateCmd(t *testing.T) {
	t.Parallel()

	runtimeContainer := &di.Runtime{}
	cmd := clusterpkg.NewUpdateCmd(runtimeContainer)

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

	yesFlag := cmd.Flags().Lookup("yes")
	if yesFlag == nil {
		t.Error("expected --yes flag to exist")
	}
}

func TestResolveForce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		forceValue bool
		yesValue   string
		expected   bool
	}{
		{name: "--force resolves to true", forceValue: true, yesValue: "", expected: true},
		{name: "--yes resolves to true", forceValue: false, yesValue: "true", expected: true},
		{
			name:       "--yes=false resolves to false",
			forceValue: false,
			yesValue:   "false",
			expected:   false,
		},
		{name: "both flags resolve to true", forceValue: true, yesValue: "true", expected: true},
		{name: "neither flag resolves to false", forceValue: false, yesValue: "", expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtimeContainer := &di.Runtime{}
			cmd := clusterpkg.NewUpdateCmd(runtimeContainer)

			if testCase.yesValue != "" {
				_ = cmd.Flags().Set("yes", testCase.yesValue)
			}

			result := clusterpkg.ExportResolveForce(testCase.forceValue, cmd.Flags().Lookup("yes"))
			if result != testCase.expected {
				t.Errorf("expected resolveForce(%v, yes=%q) = %v, got %v",
					testCase.forceValue, testCase.yesValue, testCase.expected, result)
			}
		})
	}
}

//nolint:paralleltest // subtests override global stdin reader
func TestUpdateConfirmation_UsesConfirmPackage(t *testing.T) {
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
			name:     "user rejects with 'no'",
			input:    "no\n",
			expected: false,
		},
		{
			name:     "user rejects with empty input",
			input:    "\n",
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Not parallel: SetStdinReaderForTests overrides a process-wide global
			restore := confirm.SetStdinReaderForTests(strings.NewReader(testCase.input))
			defer restore()

			result := confirm.PromptForConfirmation(nil)

			if result != testCase.expected {
				t.Errorf("expected %v, got %v", testCase.expected, result)
			}
		})
	}
}

//nolint:paralleltest // subtests override global TTY checker
func TestUpdateConfirmation_ShouldSkipPrompt(t *testing.T) {
	tests := []struct {
		name     string
		force    bool
		isTTY    bool
		expected bool
	}{
		{name: "force skips prompt", force: true, isTTY: true, expected: true},
		{name: "force skips even non-TTY", force: true, isTTY: false, expected: true},
		{name: "non-TTY skips prompt", force: false, isTTY: false, expected: true},
		{name: "TTY without force shows prompt", force: false, isTTY: true, expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Not parallel: SetTTYCheckerForTests overrides a process-wide global
			restore := confirm.SetTTYCheckerForTests(func() bool {
				return testCase.isTTY
			})
			defer restore()

			result := confirm.ShouldSkipPrompt(testCase.force)
			if result != testCase.expected {
				t.Errorf("expected ShouldSkipPrompt(%v) = %v, got %v",
					testCase.force, testCase.expected, result)
			}
		})
	}
}
