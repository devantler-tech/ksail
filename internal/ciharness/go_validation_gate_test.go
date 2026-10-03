package ciharness_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoValidationGateWaitsForInvokedWorkflow(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	gate, found := workflow.Jobs["wait-for-validate-go"]
	require.True(t, found)
	assert.Contains(t, gate.Needs, "ci-go", "the gate must wait for the actual invoked workflow")
	assert.Contains(t, gate.If, "!cancelled()", "failed dependencies must reach the result guard")
}

func TestGoValidationGateRejectsUnverifiedResults(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	gate := workflow.Jobs["wait-for-validate-go"]
	require.Len(t, gate.Steps, 1)
	step := gate.Steps[0]
	require.NotEmpty(t, step.Run, "the gate must inspect the explicit dependency result")
	assert.Equal(t, "${{ needs.ci-go.result }}", step.Env["GO_VALIDATION_RESULT"])
	assert.Equal(
		t,
		"${{ needs.changes.outputs.govuln-allowlist }}",
		step.Env["GO_VALIDATION_SELECTED"],
	)

	tests := []struct {
		name, result, selected string
		allowed                bool
	}{
		{"successful selected validation", "success", "true", true},
		{"successful unselected validation", "success", "false", true},
		{"deliberately unselected invocation", "skipped", "false", true},
		{"selected invocation skipped", "skipped", "true", false},
		{"skip without filter evidence", "skipped", "", false},
		{"failure", "failure", "true", false},
		{"unselected failure", "failure", "false", false},
		{"cancellation", "cancelled", "true", false},
		{"pending", "in_progress", "true", false},
		{"missing result", "", "true", false},
		{"unknown result", "unknown", "true", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			output, err := runGoValidationGate(t, step.Run, test.result, test.selected)
			if test.allowed {
				require.NoError(t, err, output)
			} else {
				require.Error(t, err, output)
				assert.Contains(t, output, "::error::")
			}
		})
	}
}

func runGoValidationGate(t *testing.T, script, result, selected string) (string, error) {
	t.Helper()
	requireTestExecutable(t, "bash")

	//nolint:gosec // Executes repository-owned workflow source with fixed test inputs.
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail", "-c", script)

	command.Env = append(os.Environ(),
		"GO_VALIDATION_RESULT="+result,
		"GO_VALIDATION_SELECTED="+selected,
	)

	output, err := command.CombinedOutput()

	return string(output), err
}
