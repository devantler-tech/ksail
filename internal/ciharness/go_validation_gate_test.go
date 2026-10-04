package ciharness_test

import (
	"os"
	"os/exec"
	"strings"
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
	require.Len(t, gate.Steps, 2, "retain check verification after the explicit result guard")
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

func TestGoValidationGateRetainsCheckVerification(t *testing.T) {
	t.Parallel()
	requireGoValidationExecutable(t, "node")

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	gate := workflow.Jobs["wait-for-validate-go"]
	require.Len(t, gate.Steps, 2)
	assert.Equal(t, 30, gate.TimeoutMinutes)
	assert.Equal(t, "read", gate.Permissions["checks"])
	step := gate.Steps[1]
	assert.Equal(t, "actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3", step.Uses)
	assert.Equal(
		t,
		"${{ needs.changes.outputs.govuln-allowlist }}",
		step.Env["GO_VALIDATION_SELECTED"],
	)
	script, ok := step.With["script"].(string)
	require.True(t, ok)
	assert.Contains(t, script, "github.rest.checks.listForRef")
	assert.Contains(t, script, "context.payload.pull_request.head.sha")
	assert.Contains(
		t,
		script,
		"25 * 60 * 1000",
		"the existing check verification deadline is retained",
	)

	tests := []goValidationCheckCase{
		{"successful checks", "completed", "success", "true", true, true},
		{"failed checks", "completed", "failure", "true", true, false},
		{"cancelled checks", "completed", "cancelled", "true", true, false},
		{"unknown check conclusion", "completed", "unknown", "true", true, false},
		{"pending checks", "in_progress", "", "true", true, false},
		{"selected checks absent", "", "", "true", false, false},
		{"selection evidence absent", "", "", "", false, false},
		{"deliberately unselected checks absent", "", "", "false", false, true},
	}
	for _, test := range tests {
		for _, prefix := range []string{"a", "b"} {
			t.Run(test.name+"/"+prefix, func(t *testing.T) {
				t.Parallel()

				output, err := runGoValidationChecks(t, script, strings.Repeat(prefix, 40), test)
				if test.allowed {
					require.NoError(t, err, output)
				} else {
					require.Error(t, err, output)
					assert.Contains(t, output, "::error::")
				}
			})
		}
	}
}

type goValidationCheckCase struct {
	name, checkStatus, conclusion, selected string
	present, allowed                        bool
}

func runGoValidationChecks(
	t *testing.T,
	script, head string,
	test goValidationCheckCase,
) (string, error) {
	t.Helper()

	present := "false"
	if test.present {
		present = "true"
	}
	//nolint:gosec // Executes repository-owned workflow source with fixed mocked check results.
	command := exec.CommandContext(t.Context(), "node", "-e", goValidationCheckHarness,
		script, test.checkStatus, test.conclusion, present, head)
	command.Env = append(os.Environ(), "GO_VALIDATION_SELECTED="+test.selected)
	output, err := command.CombinedOutput()

	return string(output), err
}

const goValidationCheckHarness = `
const [script, status, conclusion, present, headSHA] = process.argv.slice(1);
let now = 0;
Date.now = () => now;
global.setTimeout = (callback, milliseconds) => { now += milliseconds; callback(); };
const check = {name: '✅ Validate Main Go and Allowlist Changes / 🧪 Test', status, conclusion};
const github = {rest: {checks: {listForRef: async ({ref}) => {
  if (ref !== headSHA) throw new Error('wrong head');
  return {data: {check_runs: present === 'true' ? [check] : []}};
}}}};
const context = {repo: {owner: 'devantler-tech', repo: 'ksail'},
  payload: {pull_request: {head: {sha: headSHA}}}};
const core = {info() {}, warning() {}, setFailed(message) {
  console.error('::error::' + message); process.exitCode = 1;
}};
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
new AsyncFunction('github', 'context', 'core', script)(github, context, core)
  .catch(error => { console.error(error); process.exitCode = 1; });
`

func runGoValidationGate(t *testing.T, script, result, selected string) (string, error) {
	t.Helper()
	requireGoValidationExecutable(t, "bash")

	//nolint:gosec // Executes repository-owned workflow source with fixed test inputs.
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail", "-c", script)

	command.Env = append(os.Environ(),
		"GO_VALIDATION_RESULT="+result,
		"GO_VALIDATION_SELECTED="+selected,
	)

	output, err := command.CombinedOutput()

	return string(output), err
}

// requireGoValidationExecutable prevents a missing runtime from silently skipping a gate contract.
func requireGoValidationExecutable(t *testing.T, name string) {
	t.Helper()

	_, err := exec.LookPath(name)
	require.NoError(t, err, "Go validation contract requires %s", name)
}
