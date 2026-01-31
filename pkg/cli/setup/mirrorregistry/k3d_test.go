package mirrorregistry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
)

//nolint:funlen // Table-driven tests with comprehensive test cases.
func TestPrepareK3dConfigWithMirrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterCfg  *v1alpha1.Cluster
		k3dConfig   *v1alpha5.SimpleConfig
		mirrorSpecs []registry.MirrorSpec
		expected    bool
		checkConfig func(*testing.T, *v1alpha5.SimpleConfig)
	}{
		{
			name: "returns true and configures mirrors for K3s distribution",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
					},
				},
			},
			k3dConfig: &v1alpha5.SimpleConfig{
				ObjectMeta: types.ObjectMeta{Name: "test-cluster"},
			},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: true,
			checkConfig: func(t *testing.T, cfg *v1alpha5.SimpleConfig) {
				t.Helper()
				assert.NotEmpty(t, cfg.Registries.Config, "registries config should be set")
			},
		},
		{
			name: "returns false for non-K3s distribution",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
					},
				},
			},
			k3dConfig: &v1alpha5.SimpleConfig{
				ObjectMeta: types.ObjectMeta{Name: "test-cluster"},
			},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: false,
		},
		{
			name: "returns false when k3dConfig is nil",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
					},
				},
			},
			k3dConfig: nil,
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: false,
		},
		{
			name: "returns false when mirrorSpecs is empty",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
					},
				},
			},
			k3dConfig: &v1alpha5.SimpleConfig{
				ObjectMeta: types.ObjectMeta{Name: "test-cluster"},
			},
			mirrorSpecs: []registry.MirrorSpec{},
			expected:    false,
		},
		{
			name: "returns true with multiple mirror specs",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
					},
				},
			},
			k3dConfig: &v1alpha5.SimpleConfig{
				ObjectMeta: types.ObjectMeta{Name: "test-cluster"},
			},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
			expected: true,
			checkConfig: func(t *testing.T, cfg *v1alpha5.SimpleConfig) {
				t.Helper()
				assert.Contains(t, cfg.Registries.Config, "docker.io")
				assert.Contains(t, cfg.Registries.Config, "ghcr.io")
			},
		},
		{
			name: "configures local registry when enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "localhost:5000", // Non-empty means enabled
						},
					},
				},
			},
			k3dConfig: &v1alpha5.SimpleConfig{
				ObjectMeta: types.ObjectMeta{Name: "test-cluster"},
			},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: true,
			checkConfig: func(t *testing.T, cfg *v1alpha5.SimpleConfig) {
				t.Helper()
				assert.NotNil(
					t,
					cfg.Registries.Create,
					"local registry create config should be set",
				)
				assert.Contains(t, cfg.Registries.Create.Name, "local-registry")
			},
		},
		{
			name: "does not configure local registry when disabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "", // Empty means disabled
						},
					},
				},
			},
			k3dConfig: &v1alpha5.SimpleConfig{
				ObjectMeta: types.ObjectMeta{Name: "test-cluster"},
			},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: true,
			checkConfig: func(t *testing.T, cfg *v1alpha5.SimpleConfig) {
				t.Helper()
				assert.Nil(
					t,
					cfg.Registries.Create,
					"local registry should not be created when disabled",
				)
			},
		},
		{
			name: "returns false for Talos distribution",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionTalos,
					},
				},
			},
			k3dConfig: &v1alpha5.SimpleConfig{
				ObjectMeta: types.ObjectMeta{Name: "test-cluster"},
			},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := mirrorregistry.PrepareK3dConfigWithMirrors(
				testCase.clusterCfg,
				testCase.k3dConfig,
				testCase.mirrorSpecs,
			)

			assert.Equal(t, testCase.expected, result)

			if testCase.checkConfig != nil && testCase.k3dConfig != nil {
				testCase.checkConfig(t, testCase.k3dConfig)
			}
		})
	}
}
