package mirrorregistry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

//nolint:funlen // Table-driven tests with comprehensive test cases.
func TestPrepareKindConfigWithMirrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterCfg  *v1alpha1.Cluster
		kindConfig  *kindv1alpha4.Cluster
		mirrorSpecs []registry.MirrorSpec
		expected    bool
	}{
		{
			name: "returns true when mirror specs provided for Vanilla distribution",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
					},
				},
			},
			kindConfig: &kindv1alpha4.Cluster{},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: true,
		},
		{
			name: "returns false when no mirror specs",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
					},
				},
			},
			kindConfig:  &kindv1alpha4.Cluster{},
			mirrorSpecs: []registry.MirrorSpec{},
			expected:    false,
		},
		{
			name: "returns false for non-Vanilla distribution",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
					},
				},
			},
			kindConfig: &kindv1alpha4.Cluster{},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: false,
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
			kindConfig: &kindv1alpha4.Cluster{},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: false,
		},
		{
			name: "returns false when kindConfig is nil",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
					},
				},
			},
			kindConfig: nil,
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expected: false,
		},
		{
			name: "returns true with multiple mirror specs",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
					},
				},
			},
			kindConfig: &kindv1alpha4.Cluster{},
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
				{Host: "gcr.io", Remote: "https://gcr.io"},
			},
			expected: true,
		},
		{
			name: "returns false when mirrorSpecs is nil",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
					},
				},
			},
			kindConfig:  &kindv1alpha4.Cluster{},
			mirrorSpecs: nil,
			expected:    false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := mirrorregistry.PrepareKindConfigWithMirrors(
				testCase.clusterCfg,
				testCase.kindConfig,
				testCase.mirrorSpecs,
			)

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestGetKindMirrorsDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configPath string
		expected   string
	}{
		{
			name:       "returns default mirrors directory when config path not set",
			configPath: "",
			expected:   "kind/mirrors",
		},
		{
			name:       "returns mirrors directory relative to config file",
			configPath: "/some/path/ksail.yaml",
			expected:   "kind/mirrors",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionVanilla,
					},
				},
			}

			result := mirrorregistry.GetKindMirrorsDir(clusterCfg)

			assert.Contains(t, result, testCase.expected)
		})
	}
}
