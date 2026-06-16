// Package v1alpha1_test provides unit tests for the v1alpha1 package.
//
//nolint:funlen // Table-driven tests are naturally long
package v1alpha1_test

import (
	"encoding/json"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// TestCluster_MarshalYAML tests that MarshalYAML correctly prunes default values.
func TestCluster_MarshalYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cluster      v1alpha1.Cluster
		wantContains []string
		wantExcludes []string
	}{
		{
			name: "minimal cluster omits all defaults",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
			},
			wantContains: []string{"kind: Cluster", "apiVersion: ksail.io/v1alpha1"},
			wantExcludes: []string{
				"distribution:",
				"cni:",
				"csi:",
				"kubeconfig:",
				"sourceDirectory:",
			},
		},
		{
			name: "cluster with distribution includes distribution",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
					},
				},
			},
			wantContains: []string{"distribution: K3s"},
		},
		{
			name: "cluster with non-default CNI includes CNI",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						CNI: v1alpha1.CNICilium,
					},
				},
			},
			wantContains: []string{"cni: Cilium"},
		},
		{
			name: "cluster with connection timeout includes timeout",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Connection: v1alpha1.Connection{
							Timeout: metav1.Duration{Duration: 5 * time.Minute},
						},
					},
				},
			},
			wantContains: []string{"timeout: 5m0s"},
		},
		{
			name: "cluster with gitops engine",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						GitOpsEngine: v1alpha1.GitOpsEngineFlux,
					},
				},
			},
			wantContains: []string{"gitOpsEngine: Flux"},
		},
		{
			name: "workload spec with custom source directory",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "manifests",
					},
				},
			},
			wantContains: []string{"sourceDirectory: manifests"},
		},
		{
			name: "workload spec with validateOnPush",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Workload: v1alpha1.WorkloadSpec{
						ValidateOnPush: true,
					},
				},
			},
			wantContains: []string{"validateOnPush: true"},
		},
		{
			name: "workload spec with tag",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Workload: v1alpha1.WorkloadSpec{
						Tag: "latest",
					},
				},
			},
			wantContains: []string{"tag: latest"},
		},
		{
			name: "cluster with editor",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Editor: "vim",
				},
			},
			wantContains: []string{"editor: vim"},
		},
		{
			name: "cluster with metadata name includes metadata",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-cluster",
				},
			},
			wantContains: []string{"metadata:", "name: my-cluster"},
		},
		{
			name: "cluster without metadata name omits metadata",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
			},
			wantExcludes: []string{"metadata:"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := yaml.Marshal(&testCase.cluster)
			require.NoError(t, err)

			yamlStr := string(got)
			for _, want := range testCase.wantContains {
				assert.Contains(t, yamlStr, want, "should contain %q", want)
			}

			for _, exclude := range testCase.wantExcludes {
				assert.NotContains(t, yamlStr, exclude, "should not contain %q", exclude)
			}
		})
	}
}

// TestCluster_MarshalJSON tests that MarshalJSON correctly prunes default values.
func TestCluster_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cluster v1alpha1.Cluster
	}{
		{
			name: "minimal cluster produces valid JSON",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
			},
		},
		{
			name: "full cluster produces valid JSON",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       v1alpha1.Kind,
					APIVersion: v1alpha1.APIVersion,
				},
				Spec: v1alpha1.Spec{
					Editor: "code --wait",
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
						CNI:          v1alpha1.CNICilium,
						CSI:          v1alpha1.CSIEnabled,
						GitOpsEngine: v1alpha1.GitOpsEngineFlux,
						Connection: v1alpha1.Connection{
							Kubeconfig: "/custom/path",
							Context:    "my-context",
							Timeout:    metav1.Duration{Duration: 10 * time.Minute},
						},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
						ValidateOnPush:  true,
					},
				},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(&testCase.cluster)
			require.NoError(t, err)

			// Verify it's valid JSON by unmarshaling
			var result map[string]any

			err = json.Unmarshal(got, &result)
			require.NoError(t, err)

			// Verify kind and apiVersion are present
			assert.Equal(t, v1alpha1.Kind, result["kind"])
			assert.Equal(t, v1alpha1.APIVersion, result["apiVersion"])
		})
	}
}

// TestCluster_MarshalRoundTrip tests that marshal/unmarshal preserves data.
func TestCluster_MarshalRoundTrip(t *testing.T) {
	t.Parallel()

	original := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "roundtrip-cluster",
		},
		Spec: v1alpha1.Spec{
			Editor: "nano",
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNICilium,
				CSI:          v1alpha1.CSIEnabled,
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				Connection: v1alpha1.Connection{
					Kubeconfig: "/custom/kubeconfig",
					Context:    "test-context",
					Timeout:    metav1.Duration{Duration: 5 * time.Minute},
				},
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "manifests",
				ValidateOnPush:  true,
			},
		},
	}

	// Marshal to YAML
	yamlData, err := yaml.Marshal(&original)
	require.NoError(t, err)

	// Unmarshal back
	var restored v1alpha1.Cluster

	err = yaml.Unmarshal(yamlData, &restored)
	require.NoError(t, err)

	// Verify key fields are preserved
	assert.Equal(t, original.Kind, restored.Kind)
	assert.Equal(t, original.APIVersion, restored.APIVersion)
	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.Spec.Editor, restored.Spec.Editor)
	assert.Equal(t, original.Spec.Cluster.Distribution, restored.Spec.Cluster.Distribution)
	assert.Equal(t, original.Spec.Cluster.CNI, restored.Spec.Cluster.CNI)
	assert.Equal(t, original.Spec.Cluster.CSI, restored.Spec.Cluster.CSI)
	assert.Equal(t, original.Spec.Cluster.GitOpsEngine, restored.Spec.Cluster.GitOpsEngine)
	assert.Equal(
		t,
		original.Spec.Cluster.Connection.Kubeconfig,
		restored.Spec.Cluster.Connection.Kubeconfig,
	)
	assert.Equal(
		t,
		original.Spec.Cluster.Connection.Context,
		restored.Spec.Cluster.Connection.Context,
	)
	assert.Equal(t, original.Spec.Workload.SourceDirectory, restored.Spec.Workload.SourceDirectory)
	assert.Equal(t, original.Spec.Workload.ValidateOnPush, restored.Spec.Workload.ValidateOnPush)
}

// TestCluster_DefaultDistributionConfigPruning tests that default distribution config is pruned.
func TestCluster_DefaultDistributionConfigPruning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		distribution    v1alpha1.Distribution
		distributionCfg string
		wantInYAML      bool
		description     string
	}{
		{
			name:            "Vanilla with kind.yaml is pruned",
			distribution:    v1alpha1.DistributionVanilla,
			distributionCfg: "kind.yaml",
			wantInYAML:      false,
			description:     "default config for Vanilla should be omitted",
		},
		{
			name:            "Vanilla with custom config is kept",
			distribution:    v1alpha1.DistributionVanilla,
			distributionCfg: "custom-kind.yaml",
			wantInYAML:      true,
			description:     "non-default config should be included",
		},
		{
			name:            "K3s with k3d.yaml is pruned",
			distribution:    v1alpha1.DistributionK3s,
			distributionCfg: "k3d.yaml",
			wantInYAML:      false,
			description:     "default config for K3s should be omitted",
		},
		{
			name:            "K3s with custom config is kept",
			distribution:    v1alpha1.DistributionK3s,
			distributionCfg: "my-k3d.yaml",
			wantInYAML:      true,
			description:     "non-default config should be included",
		},
		{
			name:            "Talos with talos is pruned",
			distribution:    v1alpha1.DistributionTalos,
			distributionCfg: "talos",
			wantInYAML:      false,
			description:     "default config for Talos should be omitted",
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
						Distribution:       testCase.distribution,
						DistributionConfig: testCase.distributionCfg,
					},
				},
			}

			yamlData, err := yaml.Marshal(&cluster)
			require.NoError(t, err)

			yamlStr := string(yamlData)
			if testCase.wantInYAML {
				assert.Contains(t, yamlStr, "distributionConfig:", testCase.description)
			} else {
				assert.NotContains(t, yamlStr, "distributionConfig:", testCase.description)
			}
		})
	}
}

// TestCluster_DefaultContextPruning tests that default context names are pruned.
func TestCluster_DefaultContextPruning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		context      string
		wantInYAML   bool
		description  string
	}{
		{
			name:         "Vanilla with kind-kind is pruned",
			distribution: v1alpha1.DistributionVanilla,
			context:      "kind-kind",
			wantInYAML:   false,
			description:  "default context for Vanilla should be omitted",
		},
		{
			name:         "Vanilla with custom context is kept",
			distribution: v1alpha1.DistributionVanilla,
			context:      "my-custom-context",
			wantInYAML:   true,
			description:  "non-default context should be included",
		},
		{
			name:         "K3s with k3d-k3d-default is pruned",
			distribution: v1alpha1.DistributionK3s,
			context:      "k3d-k3d-default",
			wantInYAML:   false,
			description:  "default context for K3s should be omitted",
		},
		{
			name:         "Talos with admin@talos-default is pruned",
			distribution: v1alpha1.DistributionTalos,
			context:      "admin@talos-default",
			wantInYAML:   false,
			description:  "default context for Talos should be omitted",
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
						Distribution: testCase.distribution,
						Connection: v1alpha1.Connection{
							Context: testCase.context,
						},
					},
				},
			}

			yamlData, err := yaml.Marshal(&cluster)
			require.NoError(t, err)

			yamlStr := string(yamlData)
			if testCase.wantInYAML {
				assert.Contains(t, yamlStr, "context:", testCase.description)
			} else {
				assert.NotContains(t, yamlStr, "context:", testCase.description)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MarshalYAML — implements the gopkg.in/yaml.v3 Marshaler interface.
// The tests above use sigs.k8s.io/yaml which marshals via JSON, so MarshalYAML
// is exercised both directly and via yamlv3.
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
				Distribution:  v1alpha1.DistributionTalos,
				ControlPlanes: 1, // default — should be pruned at cluster level
				Talos: v1alpha1.OptionsTalos{
					Config: "~/.talos/config",        // default — should be pruned
					ISO:    v1alpha1.DefaultTalosISO, // default — should be pruned
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "controlPlanes")
	assert.NotContains(t, jsonStr, "~/.talos/config")
	assert.NotContains(t, jsonStr, "125127")
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
					Version:       "v1.11.2",
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
	assert.Equal(t, "v1.11.2", talos["version"])
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
					Location:               nonDefaultHetznerLocation,
					NetworkCIDR:            "192.168.0.0/16",
					TokenEnvVar:            "MY_HCLOUD_ENV_VAR_TEST",
					SSHKeyName:             "my-key",
					NetworkName:            "my-network",
					FallbackLocations: []string{
						fallbackHetznerLocation, v1alpha1.DefaultHetznerLocation,
					},
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "cpx31")
	assert.Contains(t, jsonStr, "cx41")
	assert.Contains(t, jsonStr, nonDefaultHetznerLocation)
	assert.Contains(t, jsonStr, "192.168.0.0/16")
	assert.Contains(t, jsonStr, "MY_HCLOUD_ENV_VAR")
	assert.Contains(t, jsonStr, "my-key")
	assert.Contains(t, jsonStr, "my-network")
	assert.Contains(t, jsonStr, fallbackHetznerLocation)
}

func TestCluster_MarshalJSON_HetznerPublicNet(t *testing.T) {
	t.Parallel()

	boolFalse := false
	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1alpha1.Kind,
			APIVersion: v1alpha1.APIVersion,
		},
		Spec: v1alpha1.Spec{
			Provider: v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{
					// Only the worker IPv4 toggle is set; the other three remain nil
					// and must be pruned from the output (omitzero on a nil pointer).
					WorkerPublicIPv4: &boolFalse,
				},
			},
		},
	}

	data, err := json.Marshal(&cluster)
	require.NoError(t, err)

	jsonStr := string(data)
	// The explicit false must round-trip (not be pruned like a value-typed zero bool).
	assert.Contains(t, jsonStr, `"workerPublicIPv4":false`)
	// The other three toggles are nil and must be pruned.
	assert.NotContains(t, jsonStr, "workerPublicIPv6")
	assert.NotContains(t, jsonStr, "controlPlanePublicIPv4")
	assert.NotContains(t, jsonStr, "controlPlanePublicIPv6")
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
					FallbackLocations: []string{
						nonDefaultHetznerLocation, fallbackHetznerLocation,
					},
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

func requireMap(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()

	result, ok := value[key].(map[string]any)
	require.True(t, ok)

	return result
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

	spec := requireMap(t, result, "spec")
	clusterSpec := requireMap(t, spec, "cluster")
	talos := requireMap(t, clusterSpec, "talos")

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

	spec := requireMap(t, result, "spec")
	clusterSpec := requireMap(t, spec, "cluster")
	sops := requireMap(t, clusterSpec, "sops")

	enabled, hasEnabled := sops["enabled"].(bool)
	require.True(t, hasEnabled)
	assert.True(t, enabled)
	assert.Equal(t, "MY_KEY", sops["ageKeyEnvVar"])
}

// nonDefaultHetznerLocation is a non-default Hetzner location shared by the
// Hetzner marshal tests below.
const (
	nonDefaultHetznerLocation = "nbg1"
	fallbackHetznerLocation   = "hel1"
)

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
					Location:                     nonDefaultHetznerLocation,
					FallbackLocations:            []string{fallbackHetznerLocation},
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
