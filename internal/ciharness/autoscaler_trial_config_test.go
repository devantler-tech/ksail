package ciharness_test

import (
	"context"
	"fmt"
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

const inventoryTrialConfig = `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Talos
    provider: Hetzner
    controlPlanes: 1
  provider:
    hetzner:
      controlPlaneServerType: cx23
      workerServerType: cx23
      location: fsn1
      extraMarker: preserved
`

func TestAutoscalerTrialCapacitySupportsDefaultPrunedScaffold(t *testing.T) {
	t.Parallel()

	config := "apiVersion: ksail.io/v1alpha1\nkind: Cluster\nspec:\n  cluster:\n" +
		"    distribution: Talos\n    provider: Hetzner\n"
	output, result, err := runInventoryTrialConfig(
		t,
		config,
		"cx33",
		"nbg1",
		"Talos",
		"Hetzner",
		"true",
	)
	require.NoError(t, err, string(output))

	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(result, &parsed))
	spec, valid := parsed["spec"].(map[string]any)
	require.True(t, valid)
	cluster, valid := spec["cluster"].(map[string]any)
	require.True(t, valid)
	assert.NotContains(t, cluster, "controlPlanes")
	assert.NotContains(t, cluster, "workers")

	provider, valid := spec["provider"].(map[string]any)
	require.True(t, valid)
	assert.Equal(t, map[string]any{
		"controlPlaneServerType": "cx33", "workerServerType": "cx33", "location": "nbg1",
	}, provider["hetzner"])
}

func TestAutoscalerTrialConfigKeepsOneNodeAndBindsCapacity(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ serverType, location string }{
		{"cx23", "fsn1"},
		{"cx33", "nbg1"},
		{"cx33", "hel1"},
	} {
		t.Run(testCase.serverType+"-"+testCase.location, func(t *testing.T) {
			t.Parallel()

			output, result, err := runInventoryTrialConfig(t, inventoryTrialConfig,
				testCase.serverType, testCase.location, "Talos", "Hetzner", "true")
			require.NoError(t, err, string(output))

			var config struct {
				Spec struct {
					Cluster struct {
						ControlPlanes int `yaml:"controlPlanes"`
						Workers       int `yaml:"workers"`
					} `yaml:"cluster"`
					Provider struct {
						Hetzner struct {
							ControlPlaneServerType string `yaml:"controlPlaneServerType"`
							WorkerServerType       string `yaml:"workerServerType"`
							Location               string `yaml:"location"`
							ExtraMarker            string `yaml:"extraMarker"`
						} `yaml:"hetzner"`
					} `yaml:"provider"`
				} `yaml:"spec"`
			}
			require.NoError(t, yaml.Unmarshal(result, &config))
			assert.Equal(t, 1, config.Spec.Cluster.ControlPlanes)
			assert.Zero(t, config.Spec.Cluster.Workers)
			assert.Equal(
				t,
				testCase.serverType,
				config.Spec.Provider.Hetzner.ControlPlaneServerType,
			)
			assert.Equal(t, testCase.serverType, config.Spec.Provider.Hetzner.WorkerServerType)
			assert.Equal(t, testCase.location, config.Spec.Provider.Hetzner.Location)
			assert.Equal(t, "preserved", config.Spec.Provider.Hetzner.ExtraMarker)
		})
	}
}

func TestAutoscalerTrialConfigRejectsBudgetAndContextDrift(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, config, serverType, location, distribution, provider, init string
	}{
		{"large type", inventoryTrialConfig, "cx53", "fsn1", "Talos", "Hetzner", "true"},
		{"type injection", inventoryTrialConfig, "cx33; false", "fsn1", "Talos", "Hetzner", "true"},
		{"other region", inventoryTrialConfig, "cx33", "ash", "Talos", "Hetzner", "true"},
		{"other distribution", inventoryTrialConfig, "cx33", "fsn1", "K3s", "Hetzner", "true"},
		{"other provider", inventoryTrialConfig, "cx33", "fsn1", "Talos", "Docker", "true"},
		{"not initialized", inventoryTrialConfig, "cx33", "fsn1", "Talos", "Hetzner", "false"},
		{
			"extra control plane",
			strings.ReplaceAll(inventoryTrialConfig, "controlPlanes: 1", "controlPlanes: 3"),
			"cx33", "fsn1", "Talos", "Hetzner", "true",
		},
		{
			"extra worker",
			strings.ReplaceAll(inventoryTrialConfig, "controlPlanes: 1", "controlPlanes: 1\n    workers: 1"),
			"cx33", "fsn1", "Talos", "Hetzner", "true",
		},
		{
			"config distribution",
			strings.ReplaceAll(inventoryTrialConfig, "distribution: Talos", "distribution: K3s"),
			"cx33", "fsn1", "Talos", "Hetzner", "true",
		},
		{"malformed config", "spec: [", "cx33", "fsn1", "Talos", "Hetzner", "true"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := testCase.config
			output, result, err := runInventoryTrialConfig(t, config, testCase.serverType,
				testCase.location, testCase.distribution, testCase.provider, testCase.init)
			require.Error(t, err, string(output))
			assert.Equal(t, config, string(result), "rejection must precede config mutation")
		})
	}
}

func runInventoryTrialConfig(
	t *testing.T, config, serverType, location, distribution, provider, initialized string,
) ([]byte, []byte, error) {
	t.Helper()

	script, err := filepath.Abs("../../.github/actions/ksail-cluster/hetzner-inventory-config.sh")
	require.NoError(t, err)
	configPath := filepath.Join(t.TempDir(), "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	//nolint:gosec // Repository-owned script with bounded fixture inputs.
	cmd := exec.CommandContext(
		ctx,
		"bash",
		script,
	)

	cmd.Env = append(os.Environ(), "CONFIG="+configPath, "TRIAL_SERVER_TYPE="+serverType,
		"TRIAL_LOCATION="+location, "DISTRIBUTION="+distribution, "PROVIDER="+provider,
		"INIT="+initialized)
	output, runErr := cmd.CombinedOutput()
	result, readErr := os.ReadFile(configPath) //nolint:gosec // Test-owned temporary config.
	require.NoError(t, readErr)

	if runErr != nil {
		return output, result, fmt.Errorf("configure inventory capacity: %w", runErr)
	}

	return output, result, nil
}

func TestAutoscalerTrialCapacityReachesThePoweredOffProbe(t *testing.T) {
	t.Parallel()

	state, bin := prepareAutoscalerAuditFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(state, "ksail.yaml"), []byte("unchanged"), 0o600))

	script, err := filepath.Abs("../../.github/actions/ksail-system-test/autoscaler-audit-trial.sh")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	//nolint:gosec // Repository-owned probe with fixed fixture identity.
	cmd := exec.CommandContext(
		ctx,
		"bash",
		script,
	)
	cmd.Dir = state
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STATE="+state, "MODE=complete", "GITHUB_RUN_ID=1234",
		"CLUSTER_NAME=st-hetzner-dispatch-1234", "K8S_VERSION=v1.35.0",
		"HCLOUD_TOKEN=fixture", "EVIDENCE_DIR="+filepath.Join(state, "evidence"),
		"TRIAL_SERVER_TYPE=cx33", "TRIAL_LOCATION=nbg1")
	output, runErr := cmd.CombinedOutput()
	assertAutoscalerAuditAccepted(t, state, output, runErr)
	//nolint:gosec // Test-owned provider call record.
	calls, err := os.ReadFile(
		filepath.Join(state, "hcloud-calls"),
	)
	require.NoError(t, err)
	assert.Contains(t, string(calls), "--type cx33 --location nbg1")
	assert.Contains(t, string(calls), "--start-after-create=false --without-ipv4")
}

func TestAutoscalerTrialCapacityWiringPreservesInitializationAndDefaults(t *testing.T) {
	t.Parallel()

	var workflow hetznerWorkflow
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/workflows/system-test-hetzner.yaml"), &workflow),
	)
	assert.Equal(
		t,
		"cx23",
		workflow.On.WorkflowDispatch.Inputs["inventory_trial_server_type"].Default,
	)
	assert.Equal(t, "fsn1", workflow.On.WorkflowDispatch.Inputs["inventory_trial_location"].Default)
	trial := findHarnessStep(t, workflow.Jobs["system-test"].Steps, "🧪 Run KSail System Test")
	assert.Equal(
		t,
		"${{ inputs.inventory_trial_server_type || 'cx23' }}",
		trial.With["inventory-trial-server-type"],
	)
	assert.Equal(
		t,
		"${{ inputs.inventory_trial_location || 'fsn1' }}",
		trial.With["inventory-trial-location"],
	)

	var system, cluster compositeAction
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/actions/ksail-system-test/action.yaml"), &system),
	)
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/actions/ksail-cluster/action.yml"), &cluster),
	)
	create := findHarnessStep(t, system.Runs.Steps, "🧪 ksail cluster create")
	assert.Equal(
		t,
		"${{ inputs.test-autoscaler-inventory == 'true' && inputs.inventory-trial-server-type || '' }}",
		create.With["inventory-trial-server-type"],
	)
	assert.Equal(
		t,
		"${{ inputs.test-autoscaler-inventory == 'true' && inputs.inventory-trial-location || '' }}",
		create.With["inventory-trial-location"],
	)
	probe := findHarnessStep(
		t,
		system.Runs.Steps,
		"🧪 Hetzner autoscaler inventory — unchanged update and retry",
	)
	assert.Equal(t, "${{ inputs.inventory-trial-server-type }}", probe.Env["TRIAL_SERVER_TYPE"])
	assert.Equal(t, "${{ inputs.inventory-trial-location }}", probe.Env["TRIAL_LOCATION"])
	assert.Empty(t, cluster.Inputs["inventory-trial-server-type"].Default)
	assert.Empty(t, cluster.Inputs["inventory-trial-location"].Default)
	assertInventoryCapacityStepOrder(t, cluster.Runs.Steps)
}

func assertInventoryCapacityStepOrder(t *testing.T, steps []harnessStep) {
	t.Helper()

	initIndex, capacityIndex, createIndex := -1, -1, -1

	for index, step := range steps {
		switch step.Name {
		case "🏗️ Initialize Project":
			initIndex = index
		case "🔧 Configure bounded Hetzner inventory capacity":
			capacityIndex = index
		case "🚀 Create Cluster":
			createIndex = index
		}
	}

	require.GreaterOrEqual(t, initIndex, 0)
	require.Greater(t, capacityIndex, initIndex)
	require.Greater(t, createIndex, capacityIndex)
}
