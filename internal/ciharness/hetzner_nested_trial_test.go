package ciharness_test

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestHetznerNestedTrialSelection(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &workflow),
	)

	input, found := workflow.On.WorkflowDispatch.Inputs["nested_provider_trial"]
	require.True(t, found, "manual dispatch must offer the failed scheduled path")
	assert.Equal(t, "boolean", input.Type)
	assert.Equal(t, false, input.Default)

	steps := workflow.Jobs["system-test"].Steps
	selection := findHarnessStep(t, steps, "✅ Select bounded nested-provider trial")
	assert.Equal(t, "nested-provider", selection.ID)
	assert.Empty(t, selection.If, "selection must run for scheduled and ordinary manual runs too")
	assert.Equal(t, 1, selection.TimeoutMinutes)
	assert.False(t, selection.ContinueOnError)
	assert.Less(
		t,
		harnessStepIndex(t, steps, selection.Name),
		harnessStepIndex(t, steps, "🧪 Create Hetzner Smoke Cluster"),
	)
	assert.Less(
		t,
		harnessStepIndex(t, steps, selection.Name),
		harnessStepIndex(t, steps, "🧪 Run KSail System Test"),
	)
	assert.Equal(
		t,
		"${{ steps.nested-provider.outputs.value }}",
		findHarnessStep(t, steps, "🧪 Run KSail System Test").With["test-kubernetes-provider"],
	)
}

func TestHetznerNestedTrialInputBindings(t *testing.T) {
	t.Parallel()

	selection := hetznerNestedSelectionStep(t)

	for key, expression := range map[string]string{
		"EVENT_NAME": "${{ github.event_name }}", "MATRIX_SUFFIX": "${{ matrix.suffix }}",
		"NESTED_TRIAL": "${{ inputs.nested_provider_trial }}", "DISTRIBUTION": "${{ matrix.distribution }}",
		"INIT": "${{ matrix.init }}", "CONTROL_PLANES": "${{ inputs.control_planes }}",
		"WORKERS": "${{ inputs.workers }}", "ROLLING_RESIZE": "${{ inputs.rolling_resize_server_type }}",
		"SCHEMATIC_TRIAL":       "${{ inputs.test_schematic_rollout }}",
		"SCHEMATIC_SERVER_TYPE": "${{ inputs.schematic_server_type }}",
		"INVENTORY_TRIAL":       "${{ inputs.autoscaler_inventory_trial }}",
		"FLOATING_IP_TOGGLE":    "${{ inputs.floating_ip_toggle }}",
		"CNI":                   "${{ inputs.cni }}",
		"CSI":                   "${{ inputs.csi }}",
		"LOAD_BALANCER":         "${{ inputs.load_balancer }}",
		"METRICS_SERVER":        "${{ inputs.metrics_server }}", "POLICY_ENGINE": "${{ inputs.policy_engine }}",
		"CERT_MANAGER": "${{ inputs.cert_manager }}", "GITOPS_ENGINE": "${{ inputs.gitops_engine }}",
	} {
		assert.Equal(
			t,
			expression,
			selection.Env[key],
			"admission must read the actual selection for %s",
			key,
		)
	}
}

func TestHetznerNestedTrialAdmission(t *testing.T) {
	t.Parallel()

	selection := hetznerNestedSelectionStep(t)

	for _, scenario := range []struct {
		name      string
		overrides map[string]string
		want      string
	}{
		{name: "ordinary manual", overrides: map[string]string{"NESTED_TRIAL": "false"}, want: "false"},
		{name: "ordinary legacy manual", overrides: map[string]string{"NESTED_TRIAL": ""}, want: "false"},
		{name: "explicit nested manual", want: "true"},
		{
			name:      "scheduled default init",
			overrides: map[string]string{"EVENT_NAME": "schedule", "MATRIX_SUFFIX": "default-init", "NESTED_TRIAL": ""},
			want:      "true",
		},
		{
			name:      "scheduled other case",
			overrides: map[string]string{"EVENT_NAME": "schedule", "MATRIX_SUFFIX": "fullstack", "NESTED_TRIAL": ""},
			want:      "false",
		},
		{name: "direct provider", overrides: map[string]string{"DISTRIBUTION": "Vanilla"}},
		{name: "no init", overrides: map[string]string{"INIT": "false"}},
		{name: "multiple control planes", overrides: map[string]string{"CONTROL_PLANES": "3"}},
		{name: "workers", overrides: map[string]string{"WORKERS": "1"}},
		{name: "resize", overrides: map[string]string{"ROLLING_RESIZE": "cx33"}},
		{name: "schematic", overrides: map[string]string{"SCHEMATIC_TRIAL": "true"}},
		{name: "schematic server override", overrides: map[string]string{"SCHEMATIC_SERVER_TYPE": "cx33"}},
		{name: "inventory", overrides: map[string]string{"INVENTORY_TRIAL": "true"}},
		{name: "floating ip", overrides: map[string]string{"FLOATING_IP_TOGGLE": "true"}},
		{name: "cni override", overrides: map[string]string{"CNI": "Cilium"}},
		{name: "csi override", overrides: map[string]string{"CSI": "Enabled"}},
		{name: "load balancer override", overrides: map[string]string{"LOAD_BALANCER": "Disabled"}},
		{name: "metrics override", overrides: map[string]string{"METRICS_SERVER": "Enabled"}},
		{name: "policy override", overrides: map[string]string{"POLICY_ENGINE": "Kyverno"}},
		{name: "cert manager override", overrides: map[string]string{"CERT_MANAGER": "Enabled"}},
		{name: "gitops override", overrides: map[string]string{"GITOPS_ENGINE": "Flux"}},
		{name: "invalid boolean", overrides: map[string]string{"NESTED_TRIAL": "yes"}},
		{name: "unsupported event", overrides: map[string]string{"EVENT_NAME": "push"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			assertHetznerNestedSelection(t, selection.Run, scenario.overrides, scenario.want)
		})
	}
}

func hetznerNestedSelectionStep(t *testing.T) harnessStep {
	t.Helper()

	var workflow hetznerWorkflow
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &workflow),
	)

	return findHarnessStep(
		t,
		workflow.Jobs["system-test"].Steps,
		"✅ Select bounded nested-provider trial",
	)
}

func assertHetznerNestedSelection(
	t *testing.T,
	script string,
	overrides map[string]string,
	want string,
) {
	t.Helper()

	values := map[string]string{
		"EVENT_NAME":            "workflow_dispatch",
		"MATRIX_SUFFIX":         "dispatch",
		"NESTED_TRIAL":          "true",
		"DISTRIBUTION":          "Talos",
		"INIT":                  "true",
		"CONTROL_PLANES":        "1",
		"WORKERS":               "0",
		"ROLLING_RESIZE":        "",
		"SCHEMATIC_TRIAL":       "false",
		"SCHEMATIC_SERVER_TYPE": "Default",
		"INVENTORY_TRIAL":       "false",
		"FLOATING_IP_TOGGLE":    "false",
		"CNI":                   "Default",
		"CSI":                   "Default",
		"LOAD_BALANCER":         "Default",
		"METRICS_SERVER":        "Default",
		"POLICY_ENGINE":         "None",
		"CERT_MANAGER":          "Disabled",
		"GITOPS_ENGINE":         "None",
	}
	maps.Copy(values, overrides)

	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, root.Close()) })

	outputFile := filepath.Join(directory, "output")
	command := exec.CommandContext(t.Context(), "bash")
	command.Stdin = strings.NewReader(script)

	command.Env = append(os.Environ(), "GITHUB_OUTPUT="+outputFile)
	for key, value := range values {
		command.Env = append(command.Env, key+"="+value)
	}

	output, err := command.CombinedOutput()
	if want == "" {
		require.Error(t, err, "invalid selection must reject before provisioning: %s", output)
		assert.NoFileExists(t, outputFile, "a rejected selection must not enable the nested action")

		return
	}

	require.NoError(t, err, "%s", output)
	actual, err := root.ReadFile("output")
	require.NoError(t, err)
	assert.Equal(t, "value="+want+"\n", string(actual))
}
