package ciharness_test

import (
	"maps"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const hetznerScaleDownSelection = "✅ Validate control-plane scale-down trial selection"

func TestHetznerScaleDownTrialWiring(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &workflow),
	)

	input, found := workflow.On.WorkflowDispatch.Inputs["control_plane_scale_down"]
	require.True(t, found, "manual dispatch must offer the control-plane scale-down trial")
	assert.Equal(t, "boolean", input.Type)
	assert.Equal(t, false, input.Default)

	steps := workflow.Jobs["system-test"].Steps
	assert.Less(
		t,
		harnessStepIndex(t, steps, hetznerScaleDownSelection),
		harnessStepIndex(t, steps, "🧪 Run KSail System Test"),
		"the selection must be refused before any server is created",
	)
	assert.Equal(
		t,
		"${{ github.event_name == 'workflow_dispatch' && inputs.control_plane_scale_down || false }}",
		findHarnessStep(t, steps, "🧪 Run KSail System Test").With["control-plane-scale-down"],
	)
}

func TestHetznerScaleDownTrialAdmission(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &workflow),
	)

	selection := findHarnessStep(t, workflow.Jobs["system-test"].Steps, hetznerScaleDownSelection)

	for _, scenario := range []struct {
		name      string
		overrides map[string]string
		admitted  bool
	}{
		{name: "four control planes", admitted: true},
		{name: "five control planes", overrides: map[string]string{"CONTROL_PLANES": "5"}, admitted: true},
		{name: "three control planes", overrides: map[string]string{"CONTROL_PLANES": "3"}},
		{name: "default control planes", overrides: map[string]string{"CONTROL_PLANES": ""}},
		{name: "non-numeric control planes", overrides: map[string]string{"CONTROL_PLANES": "4x"}},
		{name: "direct provider", overrides: map[string]string{"DISTRIBUTION": "K3s"}},
		{name: "resize", overrides: map[string]string{"ROLLING_RESIZE": "cx33"}},
		{name: "floating ip toggle", overrides: map[string]string{"FLOATING_IP_TOGGLE": "true"}},
		{name: "inventory", overrides: map[string]string{"INVENTORY_TRIAL": "true"}},
		{name: "schematic", overrides: map[string]string{"SCHEMATIC_TRIAL": "true"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			assertHetznerScaleDownAdmission(t, selection.Run, scenario.overrides, scenario.admitted)
		})
	}
}

func assertHetznerScaleDownAdmission(
	t *testing.T,
	script string,
	overrides map[string]string,
	admitted bool,
) {
	t.Helper()

	values := map[string]string{
		"DISTRIBUTION":       "Talos",
		"CONTROL_PLANES":     "4",
		"ROLLING_RESIZE":     "",
		"FLOATING_IP_TOGGLE": "false",
		"INVENTORY_TRIAL":    "false",
		"SCHEMATIC_TRIAL":    "false",
	}
	maps.Copy(values, overrides)

	command := exec.CommandContext(t.Context(), "bash")
	command.Stdin = strings.NewReader(script)

	command.Env = os.Environ()
	for key, value := range values {
		command.Env = append(command.Env, key+"="+value)
	}

	output, err := command.CombinedOutput()
	if admitted {
		require.NoError(t, err, "%s", output)

		return
	}

	require.Error(t, err, "an unsafe selection must be refused before provisioning: %s", output)
}
