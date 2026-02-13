//nolint:funlen // Table-driven benchmarks with scenarios are naturally long.
package v1alpha1_test

import (
	"encoding/json"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BenchmarkCluster_MarshalYAML benchmarks YAML marshalling of cluster configurations.
// Tests range from minimal configurations to fully-specified production clusters.
func BenchmarkCluster_MarshalYAML(b *testing.B) {
	scenarios := []struct {
		name    string
		cluster v1alpha1.Cluster
	}{
		{
			name:    "Minimal",
			cluster: *v1alpha1.NewCluster(),
		},
		{
			name: "WithBasicConfig",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.APIVersion,
					Kind:       v1alpha1.Kind,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
						Provider:     v1alpha1.ProviderDocker,
					},
				},
			},
		},
		{
			name: "WithCNI",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.APIVersion,
					Kind:       v1alpha1.Kind,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
						Provider:     v1alpha1.ProviderDocker,
						CNI:          v1alpha1.CNICilium,
					},
				},
			},
		},
		{
			name: "WithGitOps",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.APIVersion,
					Kind:       v1alpha1.Kind,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
						Provider:     v1alpha1.ProviderDocker,
						CNI:          v1alpha1.CNICilium,
						GitOpsEngine: v1alpha1.GitOpsEngineFlux,
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
						ValidateOnPush:  true,
					},
				},
			},
		},
		{
			name:    "FullProductionCluster",
			cluster: fullProductionCluster(),
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				_, err := scenario.cluster.MarshalYAML()
				if err != nil {
					b.Fatalf("MarshalYAML failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkCluster_MarshalJSON benchmarks JSON marshalling of cluster configurations.
// JSON marshalling is used internally by the YAML library.
func BenchmarkCluster_MarshalJSON(b *testing.B) {
	scenarios := []struct {
		name    string
		cluster v1alpha1.Cluster
	}{
		{
			name:    "Minimal",
			cluster: *v1alpha1.NewCluster(),
		},
		{
			name: "WithBasicConfig",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.APIVersion,
					Kind:       v1alpha1.Kind,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
						Provider:     v1alpha1.ProviderDocker,
					},
				},
			},
		},
		{
			name:    "FullProductionCluster",
			cluster: fullProductionCluster(),
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				_, err := scenario.cluster.MarshalJSON()
				if err != nil {
					b.Fatalf("MarshalJSON failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkYAMLEncode benchmarks full YAML encoding (marshal + encode).
// This represents the complete end-to-end performance of yaml.Marshal.
func BenchmarkYAMLEncode(b *testing.B) {
	scenarios := []struct {
		name    string
		cluster v1alpha1.Cluster
	}{
		{
			name:    "Minimal",
			cluster: *v1alpha1.NewCluster(),
		},
		{
			name: "FullProductionCluster",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.APIVersion,
					Kind:       v1alpha1.Kind,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionTalos,
						Provider:     v1alpha1.ProviderHetzner,
						CNI:          v1alpha1.CNICilium,
						GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
					},
				},
			},
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				_, err := yaml.Marshal(&scenario.cluster)
				if err != nil {
					b.Fatalf("yaml.Marshal failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkJSONEncode benchmarks full JSON encoding.
func BenchmarkJSONEncode(b *testing.B) {
	cluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.Kind,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionTalos,
				Provider:     v1alpha1.ProviderHetzner,
				CNI:          v1alpha1.CNICilium,
			},
		},
	}

	b.ReportAllocs()

	for range b.N {
		_, err := json.Marshal(&cluster)
		if err != nil {
			b.Fatalf("json.Marshal failed: %v", err)
		}
	}
}

// BenchmarkPruneClusterDefaults benchmarks the default pruning operation.
// This is a critical hot path in the marshalling process.
func BenchmarkPruneClusterDefaults(b *testing.B) {
	scenarios := []struct {
		name    string
		cluster v1alpha1.Cluster
	}{
		{
			name:    "MostlyDefaults",
			cluster: *v1alpha1.NewCluster(),
		},
		{
			name: "MixedDefaultsAndCustom",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.APIVersion,
					Kind:       v1alpha1.Kind,
				},
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
						Provider:     v1alpha1.ProviderDocker,
						CNI:          v1alpha1.CNICilium,
						Connection: v1alpha1.Connection{
							Kubeconfig: "~/.kube/config",
						},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			},
		},
		{
			name: "AllCustomValues",
			cluster: v1alpha1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha1.APIVersion,
					Kind:       v1alpha1.Kind,
				},
				Spec: v1alpha1.Spec{
					Editor: "vim",
					Cluster: v1alpha1.ClusterSpec{
						Distribution:       v1alpha1.DistributionTalos,
						Provider:           v1alpha1.ProviderHetzner,
						DistributionConfig: "custom-talos.yaml",
						Connection: v1alpha1.Connection{
							Kubeconfig: "/custom/kubeconfig",
							Context:    "custom-context",
							Timeout:    metav1.Duration{Duration: 600000000000},
						},
						CNI: v1alpha1.CNICilium,
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "custom-manifests",
						ValidateOnPush:  true,
					},
					Chat: v1alpha1.ChatSpec{
						Model: "claude-sonnet-4.5",
					},
				},
			},
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				_ = v1alpha1.PruneClusterDefaultsForTest(scenario.cluster)
			}
		})
	}
}

// fullProductionCluster returns a fully-specified production cluster configuration
// for use in multiple benchmark scenarios.
func fullProductionCluster() v1alpha1.Cluster {
	return v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.Kind,
		},
		Spec: v1alpha1.Spec{
			Editor: "code --wait",
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionTalos,
				Provider:           v1alpha1.ProviderHetzner,
				DistributionConfig: "talos.yaml",
				Connection: v1alpha1.Connection{
					Kubeconfig: "~/.kube/config",
					Context:    "talos-production",
					Timeout:    metav1.Duration{Duration: 300000000000}, // 5m
				},
				CNI:           v1alpha1.CNICilium,
				CSI:           v1alpha1.CSIEnabled,
				MetricsServer: v1alpha1.MetricsServerEnabled,
				LoadBalancer:  v1alpha1.LoadBalancerEnabled,
				CertManager:   v1alpha1.CertManagerEnabled,
				PolicyEngine:  v1alpha1.PolicyEngineKyverno,
				GitOpsEngine:  v1alpha1.GitOpsEngineArgoCD,
				Talos: v1alpha1.OptionsTalos{
					ControlPlanes: 3,
					Workers:       2,
				},
				Hetzner: v1alpha1.OptionsHetzner{
					Location:               "nbg1",
					SSHKeyName:             "my-ssh-key",
					ControlPlaneServerType: "cx22",
					WorkerServerType:       "cx32",
				},
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "manifests",
				ValidateOnPush:  true,
			},
			Chat: v1alpha1.ChatSpec{
				Model: "gpt-5",
			},
		},
	}
}
