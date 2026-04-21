package v1alpha1_test

import (
	"encoding/json"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---------------------------------------------------------------------------
// CDI.Set() — 0% coverage (no TestCDI_Set exists in enums_test.go)
// ---------------------------------------------------------------------------

//nolint:funlen // Table-driven enum parsing cases are easier to read inline.
func TestCDI_Set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantValue v1alpha1.CDI
		wantError bool
	}{
		{
			name:      "sets_default",
			input:     "Default",
			wantValue: v1alpha1.CDIDefault,
		},
		{
			name:      "sets_enabled",
			input:     "Enabled",
			wantValue: v1alpha1.CDIEnabled,
		},
		{
			name:      "sets_disabled",
			input:     "Disabled",
			wantValue: v1alpha1.CDIDisabled,
		},
		{
			name:      "case_insensitive_enabled",
			input:     "enabled",
			wantValue: v1alpha1.CDIEnabled,
		},
		{
			name:      "case_insensitive_disabled",
			input:     "DISABLED",
			wantValue: v1alpha1.CDIDisabled,
		},
		{
			name:      "case_insensitive_default",
			input:     "dEfAuLt",
			wantValue: v1alpha1.CDIDefault,
		},
		{
			name:      "invalid_value_returns_error",
			input:     "invalid",
			wantError: true,
		},
		{
			name:      "empty_value_returns_error",
			input:     "",
			wantError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var cdi v1alpha1.CDI

			err := cdi.Set(testCase.input)

			if testCase.wantError {
				require.Error(t, err)
				require.ErrorIs(t, err, v1alpha1.ErrInvalidCDI)
				assert.Contains(t, err.Error(), "Default")
				assert.Contains(t, err.Error(), "Enabled")
				assert.Contains(t, err.Error(), "Disabled")
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.wantValue, cdi)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidCDIs() — 0% coverage
// ---------------------------------------------------------------------------

func TestValidCDIs(t *testing.T) {
	t.Parallel()

	cdis := v1alpha1.ValidCDIs()

	assert.Contains(t, cdis, v1alpha1.CDIDefault)
	assert.Contains(t, cdis, v1alpha1.CDIEnabled)
	assert.Contains(t, cdis, v1alpha1.CDIDisabled)
	assert.Len(t, cdis, 3)
}

// ---------------------------------------------------------------------------
// Distribution.ContextName() — 87.5% (missing VCluster branch)
// ---------------------------------------------------------------------------

func TestDistribution_ContextName_VCluster(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.DistributionVCluster
	result := dist.ContextName("my-cluster")
	assert.Equal(t, "vcluster-docker_my-cluster", result)
}

// ---------------------------------------------------------------------------
// Distribution.DefaultClusterName() — 83.3% (missing VCluster branch)
// The existing test in enums_test.go covers Vanilla, K3s, Talos, Unknown.
// ---------------------------------------------------------------------------

func TestDistribution_DefaultClusterName_VCluster(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.DistributionVCluster
	result := dist.DefaultClusterName()
	assert.Equal(t, "vcluster-default", result)
}

// ---------------------------------------------------------------------------
// ExpectedDistributionConfigName() — 83.3% (missing VCluster branch)
// The existing test covers Vanilla, K3s, Talos, Unknown.
// ---------------------------------------------------------------------------

func TestExpectedDistributionConfigName_VCluster(t *testing.T) {
	t.Parallel()

	result := v1alpha1.ExpectedDistributionConfigName(v1alpha1.DistributionVCluster)
	assert.Equal(t, "vcluster.yaml", result)
}

// ---------------------------------------------------------------------------
// ExpectedContextName() — 83.3% (missing VCluster branch)
// The existing test covers Vanilla, K3s, Talos, Unknown.
// ---------------------------------------------------------------------------

func TestExpectedContextName_VCluster(t *testing.T) {
	t.Parallel()

	result := v1alpha1.ExpectedContextName(v1alpha1.DistributionVCluster)
	assert.Equal(t, "vcluster-docker_vcluster-default", result)
}

// ---------------------------------------------------------------------------
// Distribution.ProvidesCDIByDefault() — 75% (missing default/unknown branch)
// The existing test covers Talos, Vanilla, K3s, VCluster but not unknown.
// ---------------------------------------------------------------------------

func TestDistribution_ProvidesCDIByDefault_Unknown(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.Distribution("unknown")
	assert.False(t, dist.ProvidesCDIByDefault())

	empty := v1alpha1.Distribution("")
	assert.False(t, empty.ProvidesCDIByDefault())
}

// ---------------------------------------------------------------------------
// Distribution.ProvidesCSIByDefault() — 80% (missing unknown/default branch)
// The existing test covers K3s, Vanilla, Talos, VCluster but not unknown.
// ---------------------------------------------------------------------------

func TestDistribution_ProvidesCSIByDefault_Unknown(t *testing.T) {
	t.Parallel()

	dist := v1alpha1.Distribution("unknown")
	assert.False(t, dist.ProvidesCSIByDefault(v1alpha1.ProviderDocker))
}

// ---------------------------------------------------------------------------
// MarshalYAML() — 0% coverage
// MarshalYAML implements the gopkg.in/yaml.v3 Marshaler interface.
// The existing tests use sigs.k8s.io/yaml which marshals via JSON,
// so MarshalYAML is never exercised. We call it directly and via yamlv3.
// ---------------------------------------------------------------------------

func TestCluster_MarshalYAML_Direct(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNICilium,
			},
		},
	}

	// Call MarshalYAML directly to exercise the method
	result, err := cluster.MarshalYAML()
	require.NoError(t, err)
	require.NotNil(t, result)

	// The result should be a map[string]any
	m, ok := result.(map[string]any)
	require.True(t, ok, "MarshalYAML should return map[string]any")
	assert.Equal(t, v1alpha1.Kind, m["kind"])
	assert.Equal(t, v1alpha1.APIVersion, m["apiVersion"])
}

func TestCluster_MarshalYAML_ViaYAMLv3(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionTalos,
			},
		},
	}

	// Marshal using gopkg.in/yaml.v3 which calls MarshalYAML
	data, err := yamlv3.Marshal(&cluster)
	require.NoError(t, err)

	yamlStr := string(data)
	assert.Contains(t, yamlStr, "kind: Cluster")
	assert.Contains(t, yamlStr, "apiVersion: ksail.io/v1alpha1")
	assert.Contains(t, yamlStr, "distribution: Talos")
}

// ---------------------------------------------------------------------------
// Marshal: Talos int/int64 fields — exercises convertInt, pruneIntDefault,
// pruneByDefaultTag for int types
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_TalosDefaultsPruned(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionTalos,
				Talos: v1alpha1.OptionsTalos{
					ControlPlanes: 1,                        // default — should be pruned
					Config:        "~/.talos/config",        // default — should be pruned
					ISO:           v1alpha1.DefaultTalosISO, // default — should be pruned
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "controlPlanes")
	assert.NotContains(t, jsonStr, "~/.talos/config")
	assert.NotContains(t, jsonStr, "122630")
}

func TestCluster_MarshalJSON_TalosNonDefaultsKept(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionTalos,
				Talos: v1alpha1.OptionsTalos{
					ControlPlanes: 3,
					Workers:       5,
					Config:        "/custom/talosconfig",
					ISO:           999999,
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	var result map[string]any

	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	spec, hasSpec := result["spec"].(map[string]any)
	require.True(t, hasSpec)

	clusterSpec, hasClusterSpec := spec["cluster"].(map[string]any)
	require.True(t, hasClusterSpec)

	talos, hasTalos := clusterSpec["talos"].(map[string]any)
	require.True(t, hasTalos)

	assert.InDelta(t, 3, talos["controlPlanes"], 0)
	assert.InDelta(t, 5, talos["workers"], 0)
	assert.Equal(t, "/custom/talosconfig", talos["config"])
	assert.InDelta(t, 999999, talos["iso"], 0)
}

// ---------------------------------------------------------------------------
// Marshal: Hetzner defaults — exercises pruneByDefaultTag for string fields
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_HetznerDefaultsPruned(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Provider: v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{
					ControlPlaneServerType: v1alpha1.DefaultHetznerServerType,
					WorkerServerType:       v1alpha1.DefaultHetznerServerType,
					Location:               v1alpha1.DefaultHetznerLocation,
					NetworkCIDR:            v1alpha1.DefaultHetznerNetworkCIDR,
					TokenEnvVar:            v1alpha1.DefaultHetznerTokenEnvVar,
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "cx23")
	assert.NotContains(t, jsonStr, "fsn1")
	assert.NotContains(t, jsonStr, "10.0.0.0/16")
	assert.NotContains(t, jsonStr, "HCLOUD_TOKEN")
}

func TestCluster_MarshalJSON_HetznerNonDefaultsKept(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Provider: v1alpha1.ProviderSpec{
				//nolint:gosec // env var name fixture, not a credential.
				Hetzner: v1alpha1.OptionsHetzner{
					ControlPlaneServerType: "cpx31",
					WorkerServerType:       "cx41",
					Location:               "nbg1",
					NetworkCIDR:            "192.168.0.0/16",
					TokenEnvVar:            "MY_HCLOUD_ENV_VAR_TEST",
					SSHKeyName:             "my-key",
					NetworkName:            "my-network",
					FallbackLocations:      []string{"hel1", "fsn1"},
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "cpx31")
	assert.Contains(t, jsonStr, "cx41")
	assert.Contains(t, jsonStr, "nbg1")
	assert.Contains(t, jsonStr, "192.168.0.0/16")
	assert.Contains(t, jsonStr, "MY_HCLOUD_ENV_VAR")
	assert.Contains(t, jsonStr, "my-key")
	assert.Contains(t, jsonStr, "my-network")
	assert.Contains(t, jsonStr, "hel1")
}

// ---------------------------------------------------------------------------
// Marshal: Workload defaults — exercises pruneByDefaultTag for string/bool
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_WorkloadDefaultsPruned(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "k8s",
				ValidateOnPush:  false,
				Tag:             "dev",
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "sourceDirectory")
	assert.NotContains(t, jsonStr, "validateOnPush")
	assert.NotContains(t, jsonStr, `"tag"`)
}

// ---------------------------------------------------------------------------
// Marshal: SOPS default pruning
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_SOPSDefaultsPruned(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					AgeKeyEnvVar: v1alpha1.DefaultSOPSAgeKeyEnvVar,
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "SOPS_AGE_KEY")
}

// ---------------------------------------------------------------------------
// Marshal: Connection default pruning
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_ConnectionDefaultsPruned(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Connection: v1alpha1.Connection{
					Kubeconfig: v1alpha1.DefaultKubeconfigPath,
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "kubeconfig")
}

// ---------------------------------------------------------------------------
// Marshal: PlacementGroupStrategy Defaulter interface pruning
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_PlacementGroupStrategyDefaultPruned(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Provider: v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{
					PlacementGroupStrategy: v1alpha1.PlacementGroupStrategySpread,
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "placementGroupStrategy")
}

func TestCluster_MarshalJSON_PlacementGroupStrategyNoneKept(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Provider: v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{
					PlacementGroupStrategy: v1alpha1.PlacementGroupStrategyNone,
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "placementGroupStrategy")
	assert.Contains(t, jsonStr, "None")
}

// ---------------------------------------------------------------------------
// VCluster distribution config pruning — exercises VCluster branches in
// contextDependentPruneRules
// ---------------------------------------------------------------------------

func TestCluster_VClusterDistributionConfigPruning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		distributionCfg string
		wantInJSON      bool
	}{
		{
			name:            "vcluster.yaml_is_default_and_pruned",
			distributionCfg: "vcluster.yaml",
			wantInJSON:      false,
		},
		{
			name:            "custom_config_is_kept",
			distributionCfg: "my-vcluster.yaml",
			wantInJSON:      true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cluster := v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:       v1alpha1.DistributionVCluster,
						DistributionConfig: testCase.distributionCfg,
					},
				},
			}

			data, err := json.Marshal(&cluster)
			require.NoError(t, err)

			jsonStr := string(data)
			if testCase.wantInJSON {
				assert.Contains(t, jsonStr, "distributionConfig")
			} else {
				assert.NotContains(t, jsonStr, "distributionConfig")
			}
		})
	}
}

func TestCluster_VClusterContextPruning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		context    string
		wantInJSON bool
	}{
		{
			name:       "default_vcluster_context_is_pruned",
			context:    "vcluster-docker_vcluster-default",
			wantInJSON: false,
		},
		{
			name:       "custom_context_is_kept",
			context:    "my-custom-context",
			wantInJSON: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cluster := v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVCluster,
						Connection: v1alpha1.Connection{
							Context: testCase.context,
						},
					},
				},
			}

			data, err := json.Marshal(&cluster)
			require.NoError(t, err)

			jsonStr := string(data)
			if testCase.wantInJSON {
				assert.Contains(t, jsonStr, "context")
			} else {
				assert.NotContains(t, jsonStr, "context")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Marshal: slice fields and bool fields — exercises convertSlice, convertBool
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_WithSliceAndBoolFields(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Provider: v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{
					FallbackLocations:            []string{"nbg1", "hel1"},
					PlacementGroupFallbackToNone: true,
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	var result map[string]any

	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	spec := requireMap(t, result, "spec")
	provider := requireMap(t, spec, "provider")
	hetzner := requireMap(t, provider, "hetzner")

	fallback, ok := hetzner["fallbackLocations"].([]any)
	require.True(t, ok)
	assert.Len(t, fallback, 2)
	assert.Equal(t, true, hetzner["placementGroupFallbackToNone"])
}

// ---------------------------------------------------------------------------
// Marshal: Omni defaults pruning
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_OmniDefaultsPruned(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Provider: v1alpha1.ProviderSpec{
				Omni: v1alpha1.OptionsOmni{
					EndpointEnvVar:          "OMNI_ENDPOINT",
					ServiceAccountKeyEnvVar: "OMNI_SERVICE_ACCOUNT_KEY",
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "OMNI_ENDPOINT")
	assert.NotContains(t, jsonStr, "OMNI_SERVICE_ACCOUNT_KEY")
}

func TestCluster_MarshalJSON_OmniNonDefaultsKept(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Provider: v1alpha1.ProviderSpec{
				Omni: v1alpha1.OptionsOmni{
					Endpoint:                "https://my.omni.io:443",
					EndpointEnvVar:          "MY_OMNI_ENDPOINT",
					ServiceAccountKeyEnvVar: "MY_OMNI_KEY",
					TalosVersion:            "v1.11.2",
					KubernetesVersion:       "v1.32.0",
					MachineClass:            "my-machines",
					Machines:                []string{"machine-1", "machine-2"},
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "https://my.omni.io:443")
	assert.Contains(t, jsonStr, "MY_OMNI_ENDPOINT")
	assert.Contains(t, jsonStr, "MY_OMNI_KEY")
	assert.Contains(t, jsonStr, "v1.11.2")
	assert.Contains(t, jsonStr, "v1.32.0")
	assert.Contains(t, jsonStr, "my-machines")
	assert.Contains(t, jsonStr, "machine-1")
}

// ---------------------------------------------------------------------------
// Marshal: ExtraPortMappings — exercises convertSlice with nested structs
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_ExtraPortMappings(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionTalos,
				Talos: v1alpha1.OptionsTalos{
					ControlPlanes: 3,
					Workers:       2,
					ISO:           999999,
					ExtraPortMappings: []v1alpha1.PortMapping{
						{ContainerPort: 80, HostPort: 8080, Protocol: "UDP"},
						{ContainerPort: 443, HostPort: 8443},
					},
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	var result map[string]any

	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	spec, hasSpec := result["spec"].(map[string]any)
	require.True(t, hasSpec)

	clusterSpec, hasClusterSpec := spec["cluster"].(map[string]any)
	require.True(t, hasClusterSpec)

	talos, hasTalos := clusterSpec["talos"].(map[string]any)
	require.True(t, hasTalos)

	assert.InDelta(t, 3, talos["controlPlanes"], 0)
	assert.InDelta(t, 999999, talos["iso"], 0)

	portMappings, ok := talos["extraPortMappings"].([]any)
	require.True(t, ok)
	assert.Len(t, portMappings, 2)
}

// ---------------------------------------------------------------------------
// Marshal: SOPS.Enabled (*bool) — exercises convertValue with pointer types
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_SOPSEnabledPointer(t *testing.T) {
	t.Parallel()

	boolTrue := true
	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					AgeKeyEnvVar: "MY_KEY",
					Enabled:      &boolTrue,
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	var result map[string]any

	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	spec, hasSpec := result["spec"].(map[string]any)
	require.True(t, hasSpec)

	clusterSpec, hasClusterSpec := spec["cluster"].(map[string]any)
	require.True(t, hasClusterSpec)

	sops, hasSOPS := clusterSpec["sops"].(map[string]any)
	require.True(t, hasSOPS)

	enabled, hasEnabled := sops["enabled"].(bool)
	require.True(t, hasEnabled)
	assert.True(t, enabled)
	assert.Equal(t, "MY_KEY", sops["ageKeyEnvVar"])
}

// ---------------------------------------------------------------------------
// MarshalYAML via yamlv3 — comprehensive test exercising many field types
// ---------------------------------------------------------------------------

func TestCluster_MarshalYAML_ViaYAMLv3_Comprehensive(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Editor: "vim",
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionTalos,
				Provider:     v1alpha1.ProviderHetzner,
				CNI:          v1alpha1.CNICilium,
				CSI:          v1alpha1.CSIEnabled,
				CDI:          v1alpha1.CDIEnabled,
				Talos: v1alpha1.OptionsTalos{
					ControlPlanes: 3,
					Workers:       2,
					ISO:           999999,
					ExtraPortMappings: []v1alpha1.PortMapping{
						{ContainerPort: 80, HostPort: 8080},
					},
				},
			},
			Provider: v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{
					ControlPlaneServerType:       "cpx31",
					Location:                     "nbg1",
					FallbackLocations:            []string{"hel1"},
					PlacementGroupFallbackToNone: true,
				},
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "manifests",
				ValidateOnPush:  true,
				Tag:             "v1.0",
			},
		},
	}

	data, err := yamlv3.Marshal(&cluster)
	require.NoError(t, err)

	yamlStr := string(data)
	assert.Contains(t, yamlStr, "editor: vim")
	assert.Contains(t, yamlStr, "distribution: Talos")
	assert.Contains(t, yamlStr, "provider: Hetzner")
	assert.Contains(t, yamlStr, "cni: Cilium")
	assert.Contains(t, yamlStr, "csi: Enabled")
	assert.Contains(t, yamlStr, "cdi: Enabled")
	assert.Contains(t, yamlStr, "sourceDirectory: manifests")
	assert.Contains(t, yamlStr, "validateOnPush: true")
	assert.Contains(t, yamlStr, "tag: v1.0")
}

// ---------------------------------------------------------------------------
// Marshal: all enum defaults pruned — exercises Defaulter interface
// ---------------------------------------------------------------------------

func TestCluster_MarshalJSON_AllEnumDefaultsPruned(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:  v1alpha1.DistributionVanilla,
				Provider:      v1alpha1.ProviderDocker,
				CNI:           v1alpha1.CNIDefault,
				CSI:           v1alpha1.CSIDefault,
				CDI:           v1alpha1.CDIDefault,
				MetricsServer: v1alpha1.MetricsServerDefault,
				LoadBalancer:  v1alpha1.LoadBalancerDefault,
				GitOpsEngine:  v1alpha1.GitOpsEngineNone,
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "distribution")
	assert.NotContains(t, jsonStr, `"provider"`)
	assert.NotContains(t, jsonStr, "cni")
	assert.NotContains(t, jsonStr, "csi")
	assert.NotContains(t, jsonStr, "cdi")
	assert.NotContains(t, jsonStr, "metricsServer")
	assert.NotContains(t, jsonStr, "loadBalancer")
	assert.NotContains(t, jsonStr, "gitOpsEngine")
}

// ---------------------------------------------------------------------------
// parseHostAndPort edge cases
// ---------------------------------------------------------------------------

func TestLocalRegistry_Parse_ColonAtStart(t *testing.T) {
	t.Parallel()

	reg := v1alpha1.LocalRegistry{Registry: ":5000"}
	parsed := reg.Parse()

	// colonIdx == 0 which is <= 0, so the whole string is treated as host
	assert.Equal(t, ":5000", parsed.Host)
}

func TestLocalRegistry_Parse_InvalidPortTreatedAsHost(t *testing.T) {
	t.Parallel()

	reg := v1alpha1.LocalRegistry{Registry: "myhost:notaport"}
	parsed := reg.Parse()

	assert.Equal(t, "myhost:notaport", parsed.Host)
	assert.Equal(t, int32(0), parsed.Port)
}

// ---------------------------------------------------------------------------
// Omni provider — local registry and mirror validation
// ---------------------------------------------------------------------------

func TestValidateLocalRegistryForProvider_Omni(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		registry  v1alpha1.LocalRegistry
		wantError bool
	}{
		{
			name:      "Omni_local_registry_fails",
			registry:  v1alpha1.LocalRegistry{Registry: "localhost:5050"},
			wantError: true,
		},
		{
			name:      "Omni_external_registry_succeeds",
			registry:  v1alpha1.LocalRegistry{Registry: "ghcr.io/myorg"},
			wantError: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateLocalRegistryForProvider(
				v1alpha1.ProviderOmni,
				testCase.registry,
			)

			if testCase.wantError {
				require.ErrorIs(t, err, v1alpha1.ErrLocalRegistryNotSupported)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func requireMap(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()

	result, ok := value[key].(map[string]any)
	require.True(t, ok)

	return result
}

func TestValidateMirrorRegistriesForProvider_Omni(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mirrors   []string
		wantError bool
	}{
		{
			name:      "Omni_local_mirror_fails",
			mirrors:   []string{"docker.io=http://localhost:5000"},
			wantError: true,
		},
		{
			name:      "Omni_external_mirror_succeeds",
			mirrors:   []string{"docker.io=https://mirror.gcr.io"},
			wantError: false,
		},
		{
			name:      "Omni_ipv6_localhost_mirror_fails",
			mirrors:   []string{"docker.io=http://[::1]:5000"},
			wantError: true,
		},
		{
			name:      "Omni_0000_mirror_fails",
			mirrors:   []string{"docker.io=http://0.0.0.0:5000"},
			wantError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := v1alpha1.ValidateMirrorRegistriesForProvider(
				v1alpha1.ProviderOmni,
				testCase.mirrors,
			)

			if testCase.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
