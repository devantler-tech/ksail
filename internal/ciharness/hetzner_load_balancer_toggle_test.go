package ciharness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const loadBalancerToggleStep = "🧪 ksail cluster update — load balancers off then on"

// ksail#7678: a Hetzner dispatch can switch load balancers off and back on with cluster
// update. The option is off by default and passed to the system-test action.
func TestHetznerLoadBalancerToggleIsOptIn(t *testing.T) {
	t.Parallel()

	// Only workflow_dispatch is decoded: the schedule trigger is a list.
	//
	//nolint:tagliatelle // GitHub Actions defines this external key in snake_case.
	var trigger struct {
		On struct {
			WorkflowDispatch struct {
				Inputs map[string]struct {
					Type    string `yaml:"type"`
					Default any    `yaml:"default"`
				} `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
		} `yaml:"on"`
	}
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &trigger),
	)

	input := trigger.On.WorkflowDispatch.Inputs["load_balancer_toggle"]
	require.Equal(t, "boolean", input.Type)
	require.Equal(t, false, input.Default)

	workflow := readCIWorkflow(t, ".github/workflows/system-test-hetzner.yaml")
	system := findHarnessStep(t, workflow.Jobs["system-test"].Steps, "🧪 Run KSail System Test")
	assert.Equal(
		t,
		"${{ github.event_name == 'workflow_dispatch' && inputs.load_balancer_toggle || false }}",
		stringValue(system.With["load-balancer-toggle"]),
	)

	validate := findHarnessStep(
		t,
		workflow.Jobs["system-test"].Steps,
		"✅ Validate direct-provider smoke selection",
	)
	log, err := runStepScript(t, validate.Run, "", []string{
		"DISTRIBUTION=Vanilla", "LOAD_BALANCER_TOGGLE=true",
	})
	require.Error(t, err, log)
	assert.Contains(t, log, "load_balancer_toggle=true")
}

// The toggle checks the running cloud controller in both directions, and a failure in it
// collects the same diagnostics as the other update trials.
func TestLoadBalancerToggleChecksBothDirections(t *testing.T) {
	t.Parallel()

	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	require.Contains(t, action.Inputs, "load-balancer-toggle")
	assert.Equal(t, "false", action.Inputs["load-balancer-toggle"].Default)

	step := findHarnessStep(t, action.Runs.Steps, loadBalancerToggleStep)
	assert.Contains(t, step.If, "inputs.load-balancer-toggle == 'true'")
	assert.Contains(t, step.If, "inputs.provider == 'Hetzner'")
	assert.Contains(t, step.If, "inputs.distribution == 'Talos'")

	for _, want := range []string{
		`.spec.cluster.loadBalancer = "Disabled"`,
		"load balancers are disabled",
		`"--controllers=*,-service"`,
		"kubectl wait --for=condition=Ready nodes --all",
		"dry_run_unchanged cluster-update-load-balancer-disable-dry-run",
		"dry_run_unchanged cluster-update-load-balancer-enable-dry-run",
		"strenv(ORIGINAL)",
	} {
		assert.Contains(t, step.Run, want)
	}

	debug := findHarnessStep(t, action.Runs.Steps, "🐞 Debug Kubernetes failure")
	assert.Contains(t, debug.If, "steps.cluster-update-load-balancer-toggle.outcome == 'failure'")
}
