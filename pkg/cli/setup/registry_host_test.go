package setup

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestNeedsRegistryIPResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		clusterCfg *v1alpha1.Cluster
		expected   bool
	}{
		{
			name: "nil config",
		},
		{
			name: "talos docker local registry",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionTalos,
				v1alpha1.ProviderDocker,
				"localhost:5050",
			),
			expected: true,
		},
		{
			name: "talos default provider local registry",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionTalos,
				"",
				"localhost:5050",
			),
			expected: true,
		},
		{
			name: "talos external registry",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionTalos,
				v1alpha1.ProviderDocker,
				"ghcr.io/devantler-tech/ksail/system-test-manifests",
			),
		},
		{
			name: "talos cloud provider",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionTalos,
				v1alpha1.ProviderHetzner,
				"localhost:5050",
			),
		},
		{
			name: "vcluster docker local registry",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionVCluster,
				v1alpha1.ProviderDocker,
				"localhost:5050",
			),
			expected: true,
		},
		{
			name: "vanilla docker local registry",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionVanilla,
				v1alpha1.ProviderDocker,
				"localhost:5050",
			),
		},
		{
			name: "talos local registry disabled",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionTalos,
				v1alpha1.ProviderDocker,
				"",
			),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, needsRegistryIPResolution(testCase.clusterCfg))
		})
	}
}

func TestRegistryHostNetworkName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		clusterCfg *v1alpha1.Cluster
		cluster    string
		expected   string
	}{
		{
			name: "talos uses cluster name",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionTalos,
				v1alpha1.ProviderDocker,
				"localhost:5050",
			),
			cluster:  "talos-system-test",
			expected: "talos-system-test",
		},
		{
			name: "talos defaults empty cluster name",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionTalos,
				v1alpha1.ProviderDocker,
				"localhost:5050",
			),
			expected: defaultTalosNetworkName,
		},
		{
			name: "vcluster uses prefixed network",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionVCluster,
				v1alpha1.ProviderDocker,
				"localhost:5050",
			),
			cluster:  "dev",
			expected: "vcluster.dev",
		},
		{
			name: "other distribution has no override network",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionVanilla,
				v1alpha1.ProviderDocker,
				"localhost:5050",
			),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(
				t,
				testCase.expected,
				registryHostNetworkName(testCase.clusterCfg, testCase.cluster),
			)
		})
	}
}

func clusterForRegistryHostTest(
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
	localRegistry string,
) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: distribution,
				Provider:     provider,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: localRegistry,
				},
			},
		},
	}
}
