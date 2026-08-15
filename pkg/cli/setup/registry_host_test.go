package setup_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/stretchr/testify/assert"
)

func TestNeedsRegistryIPResolution_TrueForDockerLocalRegistries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		clusterCfg *v1alpha1.Cluster
	}{
		{
			name: "talos docker local registry",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionTalos,
				v1alpha1.ProviderDocker,
				"localhost:5050",
			),
		},
		{
			name: "talos default provider local registry",
			clusterCfg: clusterForRegistryHostTest(
				v1alpha1.DistributionTalos,
				"",
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
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.True(t, setup.NeedsRegistryIPResolutionForTest(testCase.clusterCfg))
		})
	}
}

func TestNeedsRegistryIPResolution_FalseForUnsupportedRegistryAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		clusterCfg *v1alpha1.Cluster
	}{
		{
			name: "nil config",
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

			assert.False(t, setup.NeedsRegistryIPResolutionForTest(testCase.clusterCfg))
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
			expected: setup.DefaultTalosNetworkNameForTest,
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
				setup.RegistryHostNetworkNameForTest(testCase.clusterCfg, testCase.cluster),
			)
		})
	}
}

func TestRegistryHostNetworkNameNilConfig(t *testing.T) {
	t.Parallel()

	assert.Empty(t, setup.RegistryHostNetworkNameForTest(nil, "cluster"))
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
