package setup_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/stretchr/testify/assert"
)

func TestGetComponentRequirements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		clusterCfg     *v1alpha1.Cluster
		expectedCount  int
		expectedFields map[string]bool
	}{
		{
			name: "all components disabled returns zero count",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionKind,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 0,
			expectedFields: map[string]bool{
				"NeedsMetricsServer":      false,
				"NeedsKubeletCSRApprover": false,
				"NeedsCSI":                false,
				"NeedsCertManager":        false,
				"NeedsPolicyEngine":       false,
				"NeedsArgoCD":             false,
				"NeedsFlux":               false,
			},
		},
		{
			name: "metrics-server enabled on Kind enables kubelet-csr-approver",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionKind,
						MetricsServer: v1alpha1.MetricsServerEnabled,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 2, // metrics-server + kubelet-csr-approver
			expectedFields: map[string]bool{
				"NeedsMetricsServer":      true,
				"NeedsKubeletCSRApprover": true,
				"NeedsCSI":                false,
				"NeedsCertManager":        false,
				"NeedsPolicyEngine":       false,
				"NeedsArgoCD":             false,
				"NeedsFlux":               false,
			},
		},
		{
			name: "metrics-server enabled on K3d does not enable kubelet-csr-approver",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionK3d,
						MetricsServer: v1alpha1.MetricsServerEnabled,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 0, // K3d provides metrics-server by default
			expectedFields: map[string]bool{
				"NeedsMetricsServer":      false,
				"NeedsKubeletCSRApprover": false,
				"NeedsCSI":                false,
				"NeedsCertManager":        false,
				"NeedsPolicyEngine":       false,
				"NeedsArgoCD":             false,
				"NeedsFlux":               false,
			},
		},
		{
			name: "CSI local-path-storage enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionKind,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSILocalPathStorage,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 1,
			expectedFields: map[string]bool{
				"NeedsMetricsServer":      false,
				"NeedsKubeletCSRApprover": false,
				"NeedsCSI":                true,
				"NeedsCertManager":        false,
				"NeedsPolicyEngine":       false,
				"NeedsArgoCD":             false,
				"NeedsFlux":               false,
			},
		},
		{
			name: "cert-manager enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionKind,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerEnabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 1,
			expectedFields: map[string]bool{
				"NeedsMetricsServer":      false,
				"NeedsKubeletCSRApprover": false,
				"NeedsCSI":                false,
				"NeedsCertManager":        true,
				"NeedsPolicyEngine":       false,
				"NeedsArgoCD":             false,
				"NeedsFlux":               false,
			},
		},
		{
			name: "policy-engine Kyverno enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionKind,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineKyverno,
						GitOpsEngine:  v1alpha1.GitOpsEngineNone,
					},
				},
			},
			expectedCount: 1,
			expectedFields: map[string]bool{
				"NeedsMetricsServer":      false,
				"NeedsKubeletCSRApprover": false,
				"NeedsCSI":                false,
				"NeedsCertManager":        false,
				"NeedsPolicyEngine":       true,
				"NeedsArgoCD":             false,
				"NeedsFlux":               false,
			},
		},
		{
			name: "GitOps ArgoCD enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionKind,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineArgoCD,
					},
				},
			},
			expectedCount: 1,
			expectedFields: map[string]bool{
				"NeedsMetricsServer":      false,
				"NeedsKubeletCSRApprover": false,
				"NeedsCSI":                false,
				"NeedsCertManager":        false,
				"NeedsPolicyEngine":       false,
				"NeedsArgoCD":             true,
				"NeedsFlux":               false,
			},
		},
		{
			name: "GitOps Flux enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionKind,
						MetricsServer: v1alpha1.MetricsServerDefault,
						CSI:           v1alpha1.CSIDefault,
						CertManager:   v1alpha1.CertManagerDisabled,
						PolicyEngine:  v1alpha1.PolicyEngineNone,
						GitOpsEngine:  v1alpha1.GitOpsEngineFlux,
					},
				},
			},
			expectedCount: 1,
			expectedFields: map[string]bool{
				"NeedsMetricsServer":      false,
				"NeedsKubeletCSRApprover": false,
				"NeedsCSI":                false,
				"NeedsCertManager":        false,
				"NeedsPolicyEngine":       false,
				"NeedsArgoCD":             false,
				"NeedsFlux":               true,
			},
		},
		{
			name: "all components enabled on Kind",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  v1alpha1.DistributionKind,
						MetricsServer: v1alpha1.MetricsServerEnabled,
						CSI:           v1alpha1.CSILocalPathStorage,
						CertManager:   v1alpha1.CertManagerEnabled,
						PolicyEngine:  v1alpha1.PolicyEngineKyverno,
						GitOpsEngine:  v1alpha1.GitOpsEngineFlux,
					},
				},
			},
			expectedCount: 6, // metrics-server, kubelet-csr-approver, CSI, cert-manager, policy-engine, flux
			expectedFields: map[string]bool{
				"NeedsMetricsServer":      true,
				"NeedsKubeletCSRApprover": true,
				"NeedsCSI":                true,
				"NeedsCertManager":        true,
				"NeedsPolicyEngine":       true,
				"NeedsArgoCD":             false,
				"NeedsFlux":               true,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := setup.GetComponentRequirements(testCase.clusterCfg)

			// Verify count
			assert.Equal(t, testCase.expectedCount, result.Count(), "unexpected component count")

			// Verify individual fields
			assert.Equal(
				t,
				testCase.expectedFields["NeedsMetricsServer"],
				result.NeedsMetricsServer,
				"unexpected NeedsMetricsServer value",
			)
			assert.Equal(
				t,
				testCase.expectedFields["NeedsKubeletCSRApprover"],
				result.NeedsKubeletCSRApprover,
				"unexpected NeedsKubeletCSRApprover value",
			)
			assert.Equal(t, testCase.expectedFields["NeedsCSI"], result.NeedsCSI,
				"unexpected NeedsCSI value")
			assert.Equal(t, testCase.expectedFields["NeedsCertManager"], result.NeedsCertManager,
				"unexpected NeedsCertManager value")
			assert.Equal(t, testCase.expectedFields["NeedsPolicyEngine"], result.NeedsPolicyEngine,
				"unexpected NeedsPolicyEngine value")
			assert.Equal(t, testCase.expectedFields["NeedsArgoCD"], result.NeedsArgoCD,
				"unexpected NeedsArgoCD value")
			assert.Equal(t, testCase.expectedFields["NeedsFlux"], result.NeedsFlux,
				"unexpected NeedsFlux value")
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

	// This test specifically verifies the key relationship:
	// kubelet-csr-approver is only needed when metrics-server needs installation
	// (i.e., NeedsKubeletCSRApprover == NeedsMetricsServer)

	tests := []struct {
		name          string
		distribution  v1alpha1.Distribution
		metricsServer v1alpha1.MetricsServer
		expectBoth    bool
	}{
		{
			name:          "Kind with metrics-server enabled needs both",
			distribution:  v1alpha1.DistributionKind,
			metricsServer: v1alpha1.MetricsServerEnabled,
			expectBoth:    true,
		},
		{
			name:          "Kind with metrics-server disabled needs neither",
			distribution:  v1alpha1.DistributionKind,
			metricsServer: v1alpha1.MetricsServerDisabled,
			expectBoth:    false,
		},
		{
			name:          "K3d with metrics-server enabled needs neither (K3d provides by default)",
			distribution:  v1alpha1.DistributionK3d,
			metricsServer: v1alpha1.MetricsServerEnabled,
			expectBoth:    false,
		},
		{
			name:          "Talos with metrics-server enabled needs both",
			distribution:  v1alpha1.DistributionTalos,
			metricsServer: v1alpha1.MetricsServerEnabled,
			expectBoth:    true,
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

			// Key assertion: NeedsKubeletCSRApprover should always equal NeedsMetricsServer
			assert.Equal(t, result.NeedsMetricsServer, result.NeedsKubeletCSRApprover,
				"NeedsKubeletCSRApprover should match NeedsMetricsServer")

			assert.Equal(t, testCase.expectBoth, result.NeedsMetricsServer,
				"unexpected NeedsMetricsServer value")
			assert.Equal(t, testCase.expectBoth, result.NeedsKubeletCSRApprover,
				"unexpected NeedsKubeletCSRApprover value")
		})
	}
}
