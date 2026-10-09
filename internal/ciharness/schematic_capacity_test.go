package ciharness_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfig "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSchematicTrialServerClassWiring(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(readRepoFile(t,
		".github/workflows/system-test-hetzner.yaml"), &workflow))
	selector, found := workflow.On.WorkflowDispatch.Inputs["schematic_server_type"]
	require.True(t, found)
	assert.Equal(t, "choice", selector.Type)
	assert.Equal(t, "Default", selector.Default)
	assert.ElementsMatch(t, []string{"Default", "cx23", "cx33", "cpx22"}, selector.Options)

	steps := workflow.Jobs["system-test"].Steps
	selection := findHarnessStep(t, steps, "✅ Validate Talos schematic trial selection")
	assert.Contains(t, selection.If, "inputs.schematic_server_type")
	assert.Equal(t, "${{ inputs.schematic_server_type }}", selection.Env["SERVER_TYPE"])
	assert.Equal(t, "${{ inputs.test_schematic_rollout }}", selection.Env["SCHEMATIC_TRIAL"])
	assert.Equal(
		t,
		"${{ github.event_name == 'workflow_dispatch' && inputs.schematic_server_type != 'Default'"+
			" && inputs.schematic_server_type || '' }}",
		findHarnessStep(t, steps, "🧪 Run KSail System Test").With["talos-schematic-server-type"],
	)

	var action compositeAction
	require.NoError(t, yaml.Unmarshal(readRepoFile(t,
		".github/actions/ksail-system-test/action.yaml"), &action))
	input, found := action.Inputs["talos-schematic-server-type"]
	require.True(t, found)
	assert.Empty(t, input.Default)
	resolve := findHarnessStep(t, action.Runs.Steps, "🔧 Resolve GHCR credentials in args")
	assert.Equal(t, "${{ inputs.talos-schematic-server-type }}", resolve.Env["SERVER_TYPE"])
	assert.Contains(t, resolve.Run, "SERVER_TYPE")
	assert.Equal(t, "${{ inputs.talos-schematic-server-type }}",
		findHarnessStep(t, action.Runs.Steps, "🧪 ksail cluster create").With["hetzner-server-type"])
}

func TestSchematicTrialServerClassSelectionGuard(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(t, yaml.Unmarshal(readRepoFile(t,
		".github/workflows/system-test-hetzner.yaml"), &workflow))

	selection := findHarnessStep(t, workflow.Jobs["system-test"].Steps,
		"✅ Validate Talos schematic trial selection")
	for _, scenario := range []struct {
		name, serverType, trial, distribution, init, resize string
		wantSuccess                                         bool
	}{
		{"default", "Default", "true", "Talos", "true", "", true},
		{"explicit", "cx33", "true", "Talos", "true", "", true},
		{"not-selected", "cx33", "false", "Talos", "true", "", false},
		{"distribution", "cx33", "true", "K3s", "true", "", false},
		{"init", "cx33", "true", "Talos", "false", "", false},
		{"resize", "cx33", "true", "Talos", "true", "cx43", false},
		{"invalid", "cx53", "true", "Talos", "true", "", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			commandContext, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			command := exec.CommandContext( //nolint:gosec // Executes the reviewed workflow body.
				commandContext, "bash", "-c", selection.Run,
			)

			command.Env = append(os.Environ(), "SERVER_TYPE="+scenario.serverType,
				"SCHEMATIC_TRIAL="+scenario.trial, "DISTRIBUTION="+scenario.distribution,
				"INIT="+scenario.init, "RESIZE_TYPE="+scenario.resize)

			output, err := command.CombinedOutput()
			if scenario.wantSuccess {
				require.NoErrorf(t, err, "valid selection rejected: %s", output)
			} else {
				require.Errorf(t, err, "invalid selection accepted: %s", output)
			}
		})
	}
}

const trialCapacityConfig = `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: schematic-trial
spec:
  cluster:
    distribution: Talos
    provider: Hetzner
    talos:
      version: v1.12.4
      schematicId: baseline-image
  provider:
    hetzner:
      location: fsn1
  workload:
    sourceDirectory: manifests
`

func TestSchematicTrialServerClassConfigEdit(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("yq")
	require.NoError(t, err, "the harness requires the real YAML editor")

	var action compositeAction
	require.NoError(t, yaml.Unmarshal(readRepoFile(t,
		".github/actions/ksail-cluster/action.yml"), &action))
	validate := findHarnessStep(t, action.Runs.Steps, "✅ Validate Hetzner server class")
	edit := findHarnessStep(t, action.Runs.Steps, "🔧 Select Hetzner server class")
	assert.Less(t, harnessStepIndex(t, action.Runs.Steps, validate.Name),
		harnessStepIndex(t, action.Runs.Steps, "🏗️ Initialize Project"))
	assert.Less(t, harnessStepIndex(t, action.Runs.Steps, "🏗️ Initialize Project"),
		harnessStepIndex(t, action.Runs.Steps, edit.Name))
	assert.Less(t, harnessStepIndex(t, action.Runs.Steps, edit.Name),
		harnessStepIndex(t, action.Runs.Steps, "🚀 Create Cluster"))

	for _, scenario := range []string{
		"cx23", "cx33", "cpx22", "empty", "invalid-class",
		"wrong-provider", "wrong-distribution", "no-init", "wrong-config-provider", "malformed",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			assertTrialCapacityConfig(t, validate.Run, edit.Run, scenario)
		})
	}
}

func assertTrialCapacityConfig(t *testing.T, validate, edit, scenario string) {
	t.Helper()

	selection := trialCapacitySelectionFor(scenario)
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	require.NoError(t, root.WriteFile("selected config.yaml", []byte(selection.config), 0o600))

	commandContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext( //nolint:gosec // Executes reviewed repository-owned action bodies.
		commandContext,
		"bash",
		"-e",
		"-c",
		validate+"\n"+edit,
	)
	command.Dir = directory
	command.Env = append(
		os.Environ(),
		"SERVER_TYPE="+selection.serverType,
		"PROVIDER="+selection.provider,
		"DISTRIBUTION="+selection.distribution,
		"INIT="+selection.init,
		"CONFIG="+filepath.Join(directory, "selected config.yaml"),
	)
	output, runErr := command.CombinedOutput()
	result, err := root.ReadFile("selected config.yaml")
	require.NoError(t, err)

	if !selection.wantSuccess {
		require.Errorf(t, runErr, "invalid selection accepted: %s", output)
		assert.Equal(t, selection.config, string(result))

		return
	}

	require.NoErrorf(t, runErr, "valid selection rejected: %s", output)

	serverType, expectedConfig := selection.serverType, selection.config
	if serverType != "" {
		expectedConfig = strings.Replace(expectedConfig, "location: fsn1",
			"location: fsn1\n      controlPlaneServerType: "+serverType+
				"\n      workerServerType: "+serverType, 1)
	}

	var expected, actual map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(expectedConfig), &expected))
	require.NoError(t, yaml.Unmarshal(result, &actual))
	assert.Equal(t, expected, actual, "only the two requested server classes may change")
	assertTrialCapacityLoaded(t, directory, serverType)
}

func assertTrialCapacityLoaded(t *testing.T, directory, serverType string) {
	t.Helper()

	if serverType != "" {
		manager := ksailconfig.NewConfigManager(
			io.Discard,
			filepath.Join(directory, "selected config.yaml"),
		)
		_, err := manager.Load(configmanager.LoadOptions{Silent: true, SkipValidation: true})
		require.NoError(t, err)
		assert.Equal(t, serverType, manager.Config.Spec.Provider.Hetzner.ControlPlaneServerType)
		assert.Equal(t, serverType, manager.Config.Spec.Provider.Hetzner.WorkerServerType)
		assert.Equal(t, "v1.12.4", manager.Config.Spec.Cluster.Talos.Version)
	}
}

type trialCapacitySelection struct {
	serverType, provider, distribution, init, config string
	wantSuccess                                      bool
}

func trialCapacitySelectionFor(scenario string) trialCapacitySelection {
	selection := trialCapacitySelection{
		serverType: "cx33", provider: "Hetzner", distribution: "Talos", init: "true",
		config: trialCapacityConfig,
	}

	switch scenario {
	case "cx23", "cx33", "cpx22":
		selection.serverType, selection.wantSuccess = scenario, true
	case "empty":
		selection.serverType, selection.wantSuccess = "", true
	case "invalid-class":
		selection.serverType = "cx53; exit 0"
	case "wrong-provider":
		selection.provider = "Docker"
	case "wrong-distribution":
		selection.distribution = "K3s"
	case "no-init":
		selection.init = "false"
	case "wrong-config-provider":
		selection.config = strings.Replace(
			selection.config,
			"provider: Hetzner",
			"provider: Docker",
			1,
		)
	case "malformed":
		selection.config = "spec: [invalid"
	}

	return selection
}
