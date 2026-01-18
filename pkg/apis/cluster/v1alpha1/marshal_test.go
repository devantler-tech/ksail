// Package v1alpha1_test provides unit tests for the v1alpha1 package.
//
//nolint:funlen // Table-driven tests are naturally long
package v1alpha1_test

import (
	"encoding/json"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
						CSI:          v1alpha1.CSILocalPathStorage,
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
		Spec: v1alpha1.Spec{
			Editor: "nano",
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNICilium,
				CSI:          v1alpha1.CSILocalPathStorage,
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
