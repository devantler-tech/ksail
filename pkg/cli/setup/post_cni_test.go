package setup_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/stretchr/testify/assert"
)

func assertComponentRequirements(
	t *testing.T,
	result setup.ComponentRequirements,
	expectedCount int,
	expected setup.ComponentRequirements,
) {
	t.Helper()

	assert.Equal(t, expectedCount, result.Count(), "count")
	assert.Equal(t, expected.NeedsMetricsServer, result.NeedsMetricsServer, "MetricsServer")
	assert.Equal(t, expected.NeedsLoadBalancer, result.NeedsLoadBalancer, "LoadBalancer")
	assert.Equal(
		t, expected.NeedsKubeletCSRApprover, result.NeedsKubeletCSRApprover, "KubeletCSRApprover",
	)
	assert.Equal(t, expected.NeedsCSI, result.NeedsCSI, "CSI")
	assert.Equal(t, expected.NeedsCertManager, result.NeedsCertManager, "CertManager")
	assert.Equal(t, expected.NeedsPolicyEngine, result.NeedsPolicyEngine, "PolicyEngine")
	assert.Equal(t, expected.NeedsArgoCD, result.NeedsArgoCD, "ArgoCD")
	assert.Equal(t, expected.NeedsFlux, result.NeedsFlux, "Flux")
}

//nolint:funlen // Table-driven test with comprehensive test cases
func TestGetComponentRequirements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		clusterCfg    *v1alpha1.Cluster
		expectedCount int
		expected      setup.ComponentRequirements
	}{
		{
			name: "all components disabled returns zero count",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionVanilla,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 0,
			expected:      setup.ComponentRequirements{},
		},
		{
			name: "metrics-server enabled on Kind enables kubelet-csr-approver",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionVanilla,
						MetricsServer: v1alpha1.MetricsServerEnabled,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 2, // metrics-server + kubelet-csr-approver
			expected: setup.ComponentRequirements{
				NeedsMetricsServer:      true,
				NeedsKubeletCSRApprover: true,
			},
		},
		{
			name: "metrics-server enabled on K3d does not enable kubelet-csr-approver",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionK3s,
						MetricsServer: v1alpha1.MetricsServerEnabled,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 0, // K3d provides metrics-server by default
			expected:      setup.ComponentRequirements{},
		},
		{
			name: "CSI local-path-storage enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionVanilla,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIEnabled,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 1,
			expected: setup.ComponentRequirements{
				NeedsCSI: true,
			},
		},
		{
			name: "cert-manager enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionVanilla,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerEnabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 1,
			expected: setup.ComponentRequirements{
				NeedsCertManager: true,
			},
		},
		{
			name: "policy-engine Kyverno enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionVanilla,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineKyverno,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 1,
			expected: setup.ComponentRequirements{
				NeedsPolicyEngine: true,
			},
		},
		{
			name: "GitOps ArgoCD enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionVanilla,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineArgoCD,
					},
				},
			},
			expectedCount: 1,
			expected: setup.ComponentRequirements{
				NeedsArgoCD: true,
			},
		},
		{
			name: "GitOps Flux enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionVanilla,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineFlux,
					},
				},
			},
			expectedCount: 1,
			expected: setup.ComponentRequirements{
				NeedsFlux: true,
			},
		},
		{
			name: "all components enabled on Kind",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionVanilla,
						MetricsServer: v1alpha1.MetricsServerEnabled,
						CSI:           v1alpha1.CSIEnabled,
						CertManager:   v1alpha1.CertManagerEnabled,
						PolicyEngine:  v1alpha1.PolicyEngineKyverno,
						GitOpsEngine:  v1alpha1.GitOpsEngineFlux,
					},
				},
			},
			expectedCount: 6, // metrics-server, kubelet-csr-approver, CSI, cert-manager, policy-engine, flux
			expected: setup.ComponentRequirements{
				NeedsMetricsServer:      true,
				NeedsKubeletCSRApprover: true,
				NeedsCSI:                true,
				NeedsCertManager:        true,
				NeedsPolicyEngine:       true,
				NeedsFlux:               true,
			},
		},
		{
			name: "Talos × Hetzner with LoadBalancer Default enables LoadBalancer",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionTalos,
						Provider:      v1alpha1.ProviderHetzner,
						LoadBalancer:  v1alpha1.LoadBalancerDefault,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 2, // LoadBalancer + CSI (Talos × Hetzner special case)
			expected: setup.ComponentRequirements{
				NeedsLoadBalancer: true,
				NeedsCSI:          true,
			},
		},
		{
			name: "Talos × Hetzner with LoadBalancer Enabled enables LoadBalancer",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionTalos,
						Provider:      v1alpha1.ProviderHetzner,
						LoadBalancer:  v1alpha1.LoadBalancerEnabled,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 2, // LoadBalancer + CSI (Talos × Hetzner special case)
			expected: setup.ComponentRequirements{
				NeedsLoadBalancer: true,
				NeedsCSI:          true,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := setup.GetComponentRequirements(testCase.clusterCfg)
			assertComponentRequirements(t, result, testCase.expectedCount, testCase.expected)
		})
	}
}

func TestComponentRequirementsCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		reqs     setup.ComponentRequirements
		expected int
	}{
		{
			name:     "empty requirements",
			reqs:     setup.ComponentRequirements{},
			expected: 0,
		},
		{
			name: "single component",
			reqs: setup.ComponentRequirements{
				NeedsMetricsServer: true,
			},
			expected: 1,
		},
		{
			name: "metrics-server and kubelet-csr-approver together",
			reqs: setup.ComponentRequirements{
				NeedsMetricsServer:      true,
				NeedsKubeletCSRApprover: true,
			},
			expected: 2,
		},
		{
			name: "all components enabled",
			reqs: setup.ComponentRequirements{
				NeedsMetricsServer:      true,
				NeedsKubeletCSRApprover: true,
				NeedsCSI:                true,
				NeedsCertManager:        true,
				NeedsPolicyEngine:       true,
				NeedsArgoCD:             true,
				NeedsFlux:               true,
			},
			expected: 7,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, testCase.reqs.Count())
		})
	}
}

func TestKubeletCSRApproverLinkedToMetricsServer(t *testing.T) {
	t.Parallel()

	// This test verifies the kubelet-csr-approver installation logic:
	// - For Kind: kubelet-csr-approver is installed via Helm when metrics-server needs installation
	// - For Talos: kubelet-csr-approver is installed during bootstrap via extraManifests,
	//   so Helm installation is skipped (NeedsKubeletCSRApprover = false)
	// - For K3d: metrics-server is built-in, so neither is needed

	tests := []struct {
		name                     string
		distribution             v1alpha1.Distribution
		metricsServer            v1alpha1.MetricsServer
		expectMetricsServer      bool
		expectKubeletCSRApprover bool
	}{
		{
			name:                     "Kind with metrics-server enabled needs both via Helm",
			distribution:             v1alpha1.DistributionVanilla,
			metricsServer:            v1alpha1.MetricsServerEnabled,
			expectMetricsServer:      true,
			expectKubeletCSRApprover: true, // Helm install needed
		},
		{
			name:                     "Kind with metrics-server disabled needs neither",
			distribution:             v1alpha1.DistributionVanilla,
			metricsServer:            v1alpha1.MetricsServerDisabled,
			expectMetricsServer:      false,
			expectKubeletCSRApprover: false,
		},
		{
			name:                     "K3d with metrics-server enabled needs neither (K3d provides by default)",
			distribution:             v1alpha1.DistributionK3s,
			metricsServer:            v1alpha1.MetricsServerEnabled,
			expectMetricsServer:      false,
			expectKubeletCSRApprover: false,
		},
		{
			name:                     "Talos with metrics-server enabled: metrics via Helm, CSR approver via extraManifests",
			distribution:             v1alpha1.DistributionTalos,
			metricsServer:            v1alpha1.MetricsServerEnabled,
			expectMetricsServer:      true,
			expectKubeletCSRApprover: false, // Installed during bootstrap via extraManifests
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  testCase.distribution,
						MetricsServer: testCase.metricsServer,
					},
				},
			}

			result := setup.GetComponentRequirements(clusterCfg)

			assert.Equal(t, testCase.expectMetricsServer, result.NeedsMetricsServer,
				"unexpected NeedsMetricsServer value")
			assert.Equal(t, testCase.expectKubeletCSRApprover, result.NeedsKubeletCSRApprover,
				"unexpected NeedsKubeletCSRApprover value")
		})
	}
}
