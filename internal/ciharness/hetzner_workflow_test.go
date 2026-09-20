package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

//nolint:tagliatelle // GitHub Actions defines this external key in snake_case.
type hetznerWorkflow struct {
	On struct {
		WorkflowDispatch struct {
			Inputs map[string]struct {
				Type    string   `yaml:"type"`
				Options []string `yaml:"options"`
				Default any      `yaml:"default"`
			} `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
	Jobs map[string]struct {
		If       string `yaml:"if"`
		Strategy struct {
			Matrix map[string]any `yaml:"matrix"`
		} `yaml:"strategy"`
		Steps []harnessStep `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestHetznerWorkflowAllowsManualDirectProviderSmoke(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/system-test-hetzner.yaml")

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	distribution, found := workflow.On.WorkflowDispatch.Inputs["distribution"]
	require.True(t, found, "workflow dispatch must expose a distribution selector")
	assert.Equal(t, "choice", distribution.Type)
	assert.Equal(t, "Talos", distribution.Default)
	assert.ElementsMatch(t, []string{"Talos", "K3s", "Vanilla"}, distribution.Options)

	systemTest, found := workflow.Jobs["system-test"]
	require.True(t, found, "Hetzner system-test job is missing")

	include, ok := systemTest.Strategy.Matrix["include"].(string)
	require.True(t, ok, "Hetzner matrix include must remain an expression string")
	assert.Contains(t, include, "inputs.distribution")
	assert.Contains(t, include, "inputs.distribution != 'Talos'")

	args := findHarnessStep(t, systemTest.Steps, "🔧 Build args string")
	assert.Contains(t, args.Run, `matrix.smoke != true`)
}

func TestHetznerWorkflowSmokesK3sAndVanilla(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/system-test-hetzner.yaml")

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	systemTest, found := workflow.Jobs["system-test"]
	require.True(t, found, "Hetzner system-test job is missing")
	assertHetznerSmokeMatrix(t, systemTest.Strategy.Matrix)
	assertHetznerSmokeSteps(t, systemTest.Steps)

	fallback, found := workflow.Jobs["cleanup"]
	require.True(t, found, "workflow-level Hetzner cleanup job is missing")
	assert.Contains(t, fallback.If, "always()")
	assertHetznerFallbackCleanup(t, fallback.Steps)
}

func TestHetznerSmokeReadinessRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/system-test-hetzner.yaml")

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	systemTest, found := workflow.Jobs["system-test"]
	require.True(t, found, "Hetzner system-test job is missing")
	reachability := findHarnessStep(t, systemTest.Steps, "🧪 Assert Hetzner Smoke Cluster Reachable")
	assert.Equal(t, 6, reachability.TimeoutMinutes)

	fakeBin := t.TempDir()
	attemptsFile := filepath.Join(t.TempDir(), "attempts")
	writeExecutable(t, filepath.Join(fakeBin, "ksail"), `#!/usr/bin/env bash
set -euo pipefail
attempts=0
if [[ -f "${ATTEMPTS_FILE}" ]]; then
  attempts=$(<"${ATTEMPTS_FILE}")
fi
attempts=$((attempts + 1))
printf '%s' "${attempts}" > "${ATTEMPTS_FILE}"
case "${attempts}" in
  1) exit 1 ;;
  2) printf 'starting\n' ;;
  *) printf 'ok\n' ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "sleep"), "#!/usr/bin/env bash\nexit 0\n")

	// The command is parsed from this repository's workflow, never from user input.
	command := exec.Command("bash", "-c", reachability.Run) //nolint:gosec
	command.Env = append(os.Environ(), "ATTEMPTS_FILE="+attemptsFile, "PATH="+fakeBin+":"+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "readiness check failed before the API became ready:\n%s", output)

	attempts, err := os.ReadFile(attemptsFile) //nolint:gosec // The test owns this temporary path.
	require.NoError(t, err)
	assert.Equal(t, "3", string(attempts))
}

func writeExecutable(t *testing.T, path string, contents string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	require.NoError(t, os.Chmod(path, 0o700))
}

func assertHetznerSmokeMatrix(t *testing.T, matrix map[string]any) {
	t.Helper()

	include, ok := matrix["include"].(string)
	require.True(t, ok, "Hetzner matrix include must remain an expression string")
	assert.Contains(
		t, include, `"distribution":"K3s","suffix":"k3s-smoke","args":"","smoke":true`,
	)
	assert.Contains(
		t, include,
		`"distribution":"Vanilla","suffix":"vanilla-smoke","args":"","smoke":true`,
	)
}

func assertHetznerSmokeSteps(t *testing.T, steps []harnessStep) {
	t.Helper()

	fullTest := findHarnessStep(t, steps, "🧪 Run KSail System Test")
	assert.Contains(t, fullTest.If, "matrix.smoke != true")
	assert.Equal(t, "${{ matrix.distribution }}", fullTest.With["distribution"])

	create := findHarnessStep(t, steps, "🧪 Create Hetzner Smoke Cluster")
	assert.Equal(t, "${{ matrix.smoke == true }}", create.If)
	assert.Equal(t, "$/.github/actions/ksail-cluster", create.Uses)
	assert.Equal(t, "${{ matrix.distribution }}", create.With["distribution"])
	assert.Equal(t, "Hetzner", create.With["provider"])
	assert.Equal(t, "false", create.With["init"])
	assert.Equal(t, "false", create.With["install"])
	assert.Equal(t, "false", create.With["cache"])
	assert.Equal(t, "${{ steps.args.outputs.value }}", create.With["args"])

	reachability := findHarnessStep(t, steps, "🧪 Assert Hetzner Smoke Cluster Reachable")
	assert.Equal(t, "${{ matrix.smoke == true }}", reachability.If)
	assert.Contains(t, reachability.Run, `ksail workload get --raw=/readyz`)

	cleanup := findHarnessStep(t, steps, "🧹 Delete Hetzner Smoke Cluster")
	assert.Contains(t, cleanup.If, "always()")
	assert.Contains(t, cleanup.If, "matrix.smoke == true")
	assert.Equal(t, "$/.github/actions/ksail-system-test-cleanup", cleanup.Uses)
	assert.Equal(t, "${{ secrets.HCLOUD_TOKEN }}", cleanup.Env["HCLOUD_TOKEN"])
	assert.Equal(t, "${{ matrix.distribution }}", cleanup.With["distribution"])
	assert.Equal(t, "Hetzner", cleanup.With["provider"])
}

func assertHetznerFallbackCleanup(t *testing.T, steps []harnessStep) {
	t.Helper()

	var selectors []string

	for _, step := range steps {
		if selector, selectorOK := step.With["label-selector"].(string); selectorOK {
			selectors = append(selectors, selector)
			if strings.Contains(selector, "st-hetzner-k3s-smoke-") ||
				strings.Contains(selector, "st-hetzner-vanilla-smoke-") {
				assert.Equal(t, "$/.github/actions/cleanup-hetzner", step.Uses)
			}
		}
	}

	joinedSelectors := strings.Join(selectors, "\n")
	assert.Contains(t, joinedSelectors, "st-hetzner-k3s-smoke-${{ github.run_id }}")
	assert.Contains(t, joinedSelectors, "st-hetzner-vanilla-smoke-${{ github.run_id }}")
}
