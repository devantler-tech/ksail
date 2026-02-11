package v1alpha1

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BenchmarkCluster_MarshalYAML benchmarks YAML marshalling of cluster configurations.
// Tests range from minimal configurations to fully-specified production clusters.
func BenchmarkCluster_MarshalYAML(b *testing.B) {
	scenarios := []struct {
		name    string
		cluster Cluster
	}{
		{
			name:    "Minimal",
			cluster: *NewCluster(),
		},
		{
			name: "WithBasicConfig",
			cluster: Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: APIVersion,
					Kind:       Kind,
				},
				Spec: Spec{
					Cluster: ClusterSpec{
						Distribution: DistributionVanilla,
						Provider:     ProviderDocker,
					},
				},
			},
		},
		{
			name: "WithCNI",
			cluster: Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: APIVersion,
					Kind:       Kind,
				},
				Spec: Spec{
					Cluster: ClusterSpec{
						Distribution: DistributionVanilla,
						Provider:     ProviderDocker,
						CNI:          CNICilium,
					},
				},
			},
		},
		{
			name: "WithGitOps",
			cluster: Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: APIVersion,
					Kind:       Kind,
				},
				Spec: Spec{
					Cluster: ClusterSpec{
						Distribution: DistributionK3s,
						Provider:     ProviderDocker,
						CNI:          CNICilium,
						GitOpsEngine: GitOpsEngineFlux,
					},
					Workload: WorkloadSpec{
						SourceDirectory: "k8s",
						ValidateOnPush:  true,
					},
				},
			},
		},
		{
			name: "FullProductionCluster",
			cluster: Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: APIVersion,
					Kind:       Kind,
				},
				Spec: Spec{
					Editor: "code --wait",
					Cluster: ClusterSpec{
						Distribution:       DistributionTalos,
						Provider:           ProviderHetzner,
						DistributionConfig: "talos.yaml",
						Connection: Connection{
							Kubeconfig: "~/.kube/config",
							Context:    "talos-production",
							Timeout:    metav1.Duration{Duration: 300000000000}, // 5m
						},
						CNI:           CNICilium,
						CSI:           CSIEnabled,
						MetricsServer: MetricsServerEnabled,
						LoadBalancer:  LoadBalancerEnabled,
						CertManager:   CertManagerEnabled,
						PolicyEngine:  PolicyEngineKyverno,
						GitOpsEngine:  GitOpsEngineArgoCD,
						Talos: OptionsTalos{
							ControlPlanes: 3,
							Workers:       2,
						},
						Hetzner: OptionsHetzner{
							Location:               "nbg1",
							SSHKeyName:             "my-ssh-key",
							ControlPlaneServerType: "cx22",
							WorkerServerType:       "cx32",
						},
					},
					Workload: WorkloadSpec{
						SourceDirectory: "manifests",
						ValidateOnPush:  true,
					},
					Chat: ChatSpec{
						Model: "gpt-5",
					},
				},
			},
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
		cluster Cluster
	}{
		{
			name:    "Minimal",
			cluster: *NewCluster(),
		},
		{
			name: "WithBasicConfig",
			cluster: Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: APIVersion,
					Kind:       Kind,
				},
				Spec: Spec{
					Cluster: ClusterSpec{
						Distribution: DistributionVanilla,
						Provider:     ProviderDocker,
					},
				},
			},
		},
		{
			name: "FullProductionCluster",
			cluster: Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: APIVersion,
					Kind:       Kind,
				},
				Spec: Spec{
					Editor: "code --wait",
					Cluster: ClusterSpec{
						Distribution: DistributionTalos,
						Provider:     ProviderHetzner,
						CNI:          CNICilium,
						CSI:          CSIEnabled,
						GitOpsEngine: GitOpsEngineArgoCD,
						Hetzner: OptionsHetzner{
							Location:               "nbg1",
							ControlPlaneServerType: "cx22",
							WorkerServerType:       "cx32",
						},
					},
				},
			},
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
		cluster Cluster
	}{
		{
			name:    "Minimal",
			cluster: *NewCluster(),
		},
		{
			name: "FullProductionCluster",
			cluster: Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: APIVersion,
					Kind:       Kind,
				},
				Spec: Spec{
					Cluster: ClusterSpec{
						Distribution: DistributionTalos,
						Provider:     ProviderHetzner,
						CNI:          CNICilium,
						GitOpsEngine: GitOpsEngineArgoCD,
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
	cluster := Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: APIVersion,
			Kind:       Kind,
		},
		Spec: Spec{
			Cluster: ClusterSpec{
				Distribution: DistributionTalos,
				Provider:     ProviderHetzner,
				CNI:          CNICilium,
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
		cluster Cluster
	}{
		{
			name:    "MostlyDefaults",
			cluster: *NewCluster(),
		},
		{
			name: "MixedDefaultsAndCustom",
			cluster: Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: APIVersion,
					Kind:       Kind,
				},
				Spec: Spec{
					Cluster: ClusterSpec{
						Distribution: DistributionVanilla,
						Provider:     ProviderDocker,
						CNI:          CNICilium,
						Connection: Connection{
							Kubeconfig: "~/.kube/config",
						},
					},
					Workload: WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			},
		},
		{
			name: "AllCustomValues",
			cluster: Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: APIVersion,
					Kind:       Kind,
				},
				Spec: Spec{
					Editor: "vim",
					Cluster: ClusterSpec{
						Distribution:       DistributionTalos,
						Provider:           ProviderHetzner,
						DistributionConfig: "custom-talos.yaml",
						Connection: Connection{
							Kubeconfig: "/custom/kubeconfig",
							Context:    "custom-context",
							Timeout:    metav1.Duration{Duration: 600000000000},
						},
						CNI: CNICilium,
					},
					Workload: WorkloadSpec{
						SourceDirectory: "custom-manifests",
						ValidateOnPush:  true,
					},
					Chat: ChatSpec{
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
				_ = pruneClusterDefaults(scenario.cluster)
			}
		})
	}
}
