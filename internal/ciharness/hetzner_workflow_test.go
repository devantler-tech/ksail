package ciharness_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	commandContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext(commandContext, "bash", "-c", reachability.Run) //nolint:gosec

	command.Env = append(
		os.Environ(),
		"ATTEMPTS_FILE="+attemptsFile,
		"PATH="+fakeBin+":"+os.Getenv("PATH"),
	)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "readiness check failed before the API became ready:\n%s", output)

	attempts, err := os.ReadFile(attemptsFile) //nolint:gosec // The test owns this temporary path.
	require.NoError(t, err)
	assert.Equal(t, "3", string(attempts))
}

const vanillaUserDataStep = "🔐 Verify Vanilla Control-Plane User-Data Carries No Signing Material"

func TestHetznerVanillaSmokeVerifiesNodeUserData(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/system-test-hetzner.yaml")

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(contents, &workflow))

	systemTest, found := workflow.Jobs["system-test"]
	require.True(t, found, "Hetzner system-test job is missing")

	verify := findHarnessStep(t, systemTest.Steps, vanillaUserDataStep)
	assert.Equal(t, "${{ matrix.smoke == true && matrix.distribution == 'Vanilla' }}", verify.If)
	assert.NotZero(t, verify.TimeoutMinutes)
	assert.Contains(
		t,
		verify.Env["PROBE_IMAGE"],
		"@sha256:",
		"the probe image must be pinned by digest",
	)

	// The check must run against a reachable cluster and before it is deleted.
	verifyIndex := harnessStepIndex(t, systemTest.Steps, vanillaUserDataStep)
	assert.Greater(
		t,
		verifyIndex,
		harnessStepIndex(t, systemTest.Steps, "🧪 Assert Hetzner Smoke Cluster Reachable"),
	)
	assert.Less(
		t, verifyIndex, harnessStepIndex(t, systemTest.Steps, "🧹 Delete Hetzner Smoke Cluster"),
	)

	t.Run("runs the verifier through a pod on the control-plane node", func(t *testing.T) {
		t.Parallel()

		calls, err := runVanillaUserDataStep(t, verify.Run, "cp-1")
		require.NoErrorf(t, err, "verification step failed:\n%s", calls)

		assert.Contains(t, calls, `kubectl run userdata-probe --image=probe-image`)
		assert.Contains(t, calls, `"nodeName":"cp-1","hostNetwork":true`)
		assert.Contains(
			t, calls,
			"go run ./pkg/svc/provisioner/cluster/internal/hetznerbase/cmd/verifynodeuserdata "+
				"-- kubectl exec userdata-probe -- sh -c",
		)
	})

	t.Run("fails without a control-plane node instead of skipping", func(t *testing.T) {
		t.Parallel()

		calls, err := runVanillaUserDataStep(t, verify.Run, "")
		require.Error(t, err, "a cluster with no control-plane node must fail the check")
		assert.NotContains(t, calls, "go run", "nothing may be verified without a node")
	})
}

// runVanillaUserDataStep runs the workflow step against fake kubectl and go
// commands that record their arguments, and returns the recorded calls.
func runVanillaUserDataStep(t *testing.T, script, node string) (string, error) {
	t.Helper()

	fakeBin := t.TempDir()
	callsFile := filepath.Join(t.TempDir(), "calls")

	writeExecutable(t, filepath.Join(fakeBin, "kubectl"), `#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >> "${CALLS_FILE}"
if [ "$1" = get ]; then printf '%s' "${CONTROL_PLANE_NODE}"; fi
`)
	writeExecutable(t, filepath.Join(fakeBin, "go"), `#!/usr/bin/env bash
printf 'go %s\n' "$*" >> "${CALLS_FILE}"
`)

	// The command is parsed from this repository's workflow, never from user input.
	commandContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext(commandContext, "bash", "-c", script) //nolint:gosec

	command.Env = append(
		os.Environ(),
		"CALLS_FILE="+callsFile,
		"CONTROL_PLANE_NODE="+node,
		"PROBE_IMAGE=probe-image",
		"PATH="+fakeBin+":"+os.Getenv("PATH"),
	)
	_, runErr := command.CombinedOutput()

	calls, err := os.ReadFile(callsFile) //nolint:gosec // The test owns this temporary path.
	if err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}

	return string(calls), runErr
}

func writeExecutable(t *testing.T, path string, contents string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	require.NoError(
		t,
		os.Chmod(path, 0o700), //nolint:gosec // Test-owned shell fixture must be executable.
	)
}

func assertHetznerSmokeMatrix(t *testing.T, matrix map[string]any) {
	t.Helper()

	include, ok := matrix["include"].(string)
	require.True(t, ok, "Hetzner matrix include must remain an expression string")

	const fromJSONPrefix = `fromJSON('`

	start := strings.Index(include, fromJSONPrefix)
	require.NotEqual(t, -1, start, "Hetzner matrix must contain a static fromJSON payload")

	payload := include[start+len(fromJSONPrefix):]
	end := strings.Index(payload, `')`)
	require.NotEqual(t, -1, end, "static Hetzner matrix JSON must be terminated")

	type matrixEntry struct {
		Init         bool   `json:"init"`
		Distribution string `json:"distribution"`
		Suffix       string `json:"suffix"`
		Args         string `json:"args"`
		Smoke        bool   `json:"smoke"`
	}

	var entries []matrixEntry
	require.NoError(t, json.Unmarshal([]byte(payload[:end]), &entries))

	var smokeEntries []matrixEntry

	for _, entry := range entries {
		if entry.Smoke {
			smokeEntries = append(smokeEntries, entry)
		}
	}

	assert.Equal(t, []matrixEntry{
		{Distribution: "K3s", Suffix: "k3s-smoke", Smoke: true},
		{Distribution: "Vanilla", Suffix: "vanilla-smoke", Smoke: true},
	}, smokeEntries)
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

	expected := []struct {
		name     string
		selector string
	}{
		{
			name:     "🧹 Cleanup Hetzner resources (K3s smoke)",
			selector: "ksail.cluster.name=st-hetzner-k3s-smoke-${{ github.run_id }}",
		},
		{
			name:     "🧹 Cleanup Hetzner resources (Vanilla smoke)",
			selector: "ksail.cluster.name=st-hetzner-vanilla-smoke-${{ github.run_id }}",
		},
	}

	for _, want := range expected {
		step := findHarnessStep(t, steps, want.name)
		assert.Equal(t, "$/.github/actions/cleanup-hetzner", step.Uses)
		assert.Equal(t, want.selector, step.With["label-selector"])
	}
}
