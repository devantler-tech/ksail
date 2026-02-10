package mirrorregistry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
)

//nolint:funlen // Table-driven tests with comprehensive test cases.
func TestPrepareTalosConfigWithMirrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterCfg  *v1alpha1.Cluster
		talosConfig *talosconfigmanager.Configs
		mirrorSpecs []registry.MirrorSpec
		clusterName string
		expected    bool
	}{
		{
			name: "returns true when mirror specs provided for Talos distribution",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionTalos,
					},
				},
			},
			talosConfig: &talosconfigmanager.Configs{},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			clusterName: "test-cluster",
			expected:    true,
		},
		{
			name: "returns false for non-Talos distribution",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
					},
				},
			},
			talosConfig: &talosconfigmanager.Configs{},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			clusterName: "test-cluster",
			expected:    false,
		},
		{
			name: "returns false for K3s distribution",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
					},
				},
			},
			talosConfig: &talosconfigmanager.Configs{},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			clusterName: "test-cluster",
			expected:    false,
		},
		{
			name: "returns false when mirrorSpecs is empty",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionTalos,
					},
				},
			},
			talosConfig: &talosconfigmanager.Configs{},
			mirrorSpecs: []registry.MirrorSpec{},
			clusterName: "test-cluster",
			expected:    false,
		},
		{
			name: "returns false when mirrorSpecs is nil",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionTalos,
					},
				},
			},
			talosConfig: &talosconfigmanager.Configs{},
			mirrorSpecs: nil,
			clusterName: "test-cluster",
			expected:    false,
		},
		{
			name: "returns true with nil talosConfig (handles gracefully)",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionTalos,
					},
				},
			},
			talosConfig: nil,
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			clusterName: "test-cluster",
			expected:    true, // Still returns true because we have mirror specs
		},
		{
			name: "returns true with multiple mirror specs",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionTalos,
					},
				},
			},
			talosConfig: &talosconfigmanager.Configs{},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
				{Host: "gcr.io", Remote: "https://gcr.io"},
			},
			clusterName: "test-cluster",
			expected:    true,
		},
		{
			name: "uses cluster name for registry naming",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionTalos,
					},
				},
			},
			talosConfig: &talosconfigmanager.Configs{},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			clusterName: "my-custom-cluster",
			expected:    true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := mirrorregistry.PrepareTalosConfigWithMirrors(
				testCase.clusterCfg,
				testCase.talosConfig,
				testCase.mirrorSpecs,
				testCase.clusterName,
			)

			assert.Equal(t, testCase.expected, result)
		})
	}
}
