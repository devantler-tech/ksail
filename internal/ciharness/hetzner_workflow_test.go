package ciharness_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type hetznerWorkflow struct {
	Jobs map[string]struct {
		Strategy struct {
			Matrix map[string]any `yaml:"matrix"`
		} `yaml:"strategy"`
		Steps []harnessStep `yaml:"steps"`
	} `yaml:"jobs"`
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
	assertHetznerFallbackCleanup(t, fallback.Steps)
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
	assert.Equal(t, "./.github/actions/ksail-cluster", create.Uses)
	assert.Equal(t, "${{ matrix.distribution }}", create.With["distribution"])
	assert.Equal(t, "Hetzner", create.With["provider"])
	assert.Equal(t, "false", create.With["init"])
	assert.Equal(t, "false", create.With["install"])
	assert.Equal(t, "false", create.With["cache"])
	assert.Equal(t, "${{ steps.args.outputs.value }}", create.With["args"])

	reachability := findHarnessStep(t, steps, "🧪 Assert Hetzner Smoke Cluster Reachable")
	assert.Equal(t, "${{ matrix.smoke == true }}", reachability.If)
	assert.Contains(t, reachability.Run, `ksail workload get --raw=/readyz`)
	assert.Contains(t, reachability.Run, `if [ "$READYZ" != "ok" ]`)

	cleanup := findHarnessStep(t, steps, "🧹 Delete Hetzner Smoke Cluster")
	assert.Contains(t, cleanup.If, "always()")
	assert.Contains(t, cleanup.If, "matrix.smoke == true")
	assert.Equal(t, "./.github/actions/ksail-system-test-cleanup", cleanup.Uses)
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
		}
	}

	joinedSelectors := strings.Join(selectors, "\n")
	assert.Contains(t, joinedSelectors, "st-hetzner-k3s-smoke-${{ github.run_id }}")
	assert.Contains(t, joinedSelectors, "st-hetzner-vanilla-smoke-${{ github.run_id }}")
}
