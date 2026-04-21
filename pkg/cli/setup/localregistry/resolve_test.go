package localregistry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// TestResolveClusterName exercises cluster name resolution for each distribution.
//
//nolint:funlen // table-driven test
func TestResolveClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		kindConfig   *kindv1alpha4.Cluster
		k3dConfig    *k3dv1alpha5.SimpleConfig
		context      string
		expected     string
	}{
		{
			name:         "Vanilla uses Kind config name",
			distribution: v1alpha1.DistributionVanilla,
			kindConfig:   &kindv1alpha4.Cluster{Name: "kind-test"},
			expected:     "kind-test",
		},
		{
			name:         "K3s uses K3d config name (defaults to k3d-default)",
			distribution: v1alpha1.DistributionK3s,
			k3dConfig:    &k3dv1alpha5.SimpleConfig{},
			expected:     "k3d-default",
		},
		{
			name:         "default fallback with unknown distribution returns ksail",
			distribution: v1alpha1.Distribution("unsupported"),
			context:      "my-context",
			expected:     "ksail",
		},
		{
			name:         "default fallback with empty context returns ksail",
			distribution: v1alpha1.Distribution("unsupported"),
			expected:     "ksail",
		},
		{
			name:         "KWOK strips kwok- prefix from context",
			distribution: v1alpha1.DistributionKWOK,
			context:      "kwok-test-cluster",
			expected:     "test-cluster",
		},
		{
			name:         "KWOK trims whitespace from extracted cluster name",
			distribution: v1alpha1.DistributionKWOK,
			context:      "kwok- my-cluster",
			expected:     "my-cluster",
		},
		{
			name:         "KWOK with no context returns kwok-default",
			distribution: v1alpha1.DistributionKWOK,
			expected:     "kwok-default",
		},
	}

	for _, tc := range tests { //nolint:varnamelen
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: tc.distribution,
						Connection: v1alpha1.Connection{
							Context: tc.context,
						},
					},
				},
			}

			result := localregistry.ResolveClusterNameForTest(
				clusterCfg, tc.kindConfig, tc.k3dConfig, nil, nil,
			)

			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestResolveNetworkName exercises network name resolution for each distribution.
//
//nolint:funlen // table-driven test
func TestResolveNetworkName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		clusterName  string
		expected     string
	}{
		{
			name:         "Vanilla always returns kind",
			distribution: v1alpha1.DistributionVanilla,
			clusterName:  "my-cluster",
			expected:     "kind",
		},
		{
			name:         "K3s returns k3d-prefix",
			distribution: v1alpha1.DistributionK3s,
			clusterName:  "my-cluster",
			expected:     "k3d-my-cluster",
		},
		{
			name:         "K3s with empty name returns k3d-k3d",
			distribution: v1alpha1.DistributionK3s,
			clusterName:  "",
			expected:     "k3d-k3d",
		},
		{
			name:         "Talos returns cluster name",
			distribution: v1alpha1.DistributionTalos,
			clusterName:  "my-cluster",
			expected:     "my-cluster",
		},
		{
			name:         "Talos with empty name returns talos-default",
			distribution: v1alpha1.DistributionTalos,
			clusterName:  "",
			expected:     "talos-default",
		},
		{
			name:         "VCluster returns vcluster.prefix",
			distribution: v1alpha1.DistributionVCluster,
			clusterName:  "my-cluster",
			expected:     "vcluster.my-cluster",
		},
		{
			name:         "VCluster with empty name returns vcluster.vcluster-default",
			distribution: v1alpha1.DistributionVCluster,
			clusterName:  "",
			expected:     "vcluster.vcluster-default",
		},
		{
			name:         "unknown distribution returns empty string",
			distribution: v1alpha1.Distribution("unknown"),
			clusterName:  "test",
			expected:     "",
		},
		{
			name:         "KWOK returns kwok-prefix",
			distribution: v1alpha1.DistributionKWOK,
			clusterName:  "my-cluster",
			expected:     "kwok-my-cluster",
		},
		{
			name:         "KWOK with empty name uses kwok-default fallback",
			distribution: v1alpha1.DistributionKWOK,
			clusterName:  "",
			expected:     "kwok-kwok-default",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: testCase.distribution,
					},
				},
			}

			result := localregistry.ResolveNetworkNameForTest(clusterCfg, testCase.clusterName)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// TestResolveStage exercises stage resolution for each StageType.
func TestResolveStage(t *testing.T) {
	t.Parallel()

	t.Run("StageProvision returns provision info", func(t *testing.T) {
		t.Parallel()

		info, actionBuilder, err := localregistry.ResolveStageForTest(localregistry.StageProvision)
		require.NoError(t, err)
		assert.Equal(t, "Create local registry...", info.Title)
		assert.NotNil(t, actionBuilder)
	})

	t.Run("StageConnect returns connect info", func(t *testing.T) {
		t.Parallel()

		info, actionBuilder, err := localregistry.ResolveStageForTest(localregistry.StageConnect)
		require.NoError(t, err)
		assert.Equal(t, "Attach local registry...", info.Title)
		assert.NotNil(t, actionBuilder)
	})

	t.Run("StageVerify returns error", func(t *testing.T) {
		t.Parallel()

		_, _, err := localregistry.ResolveStageForTest(localregistry.StageVerify)
		require.Error(t, err)
		assert.ErrorIs(t, err, localregistry.ErrUnsupportedStage)
	})

	t.Run("unknown stage returns error", func(t *testing.T) {
		t.Parallel()

		_, _, err := localregistry.ResolveStageForTest(localregistry.StageType(99))
		require.Error(t, err)
		assert.ErrorIs(t, err, localregistry.ErrUnsupportedStage)
	})
}

// TestShouldSkipK3d verifies K3d skip detection.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestShouldSkipK3d(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		expected     bool
	}{
		{
			name:         "K3s returns true",
			distribution: v1alpha1.DistributionK3s,
			expected:     true,
		},
		{
			name:         "Vanilla returns false",
			distribution: v1alpha1.DistributionVanilla,
			expected:     false,
		},
		{
			name:         "Talos returns false",
			distribution: v1alpha1.DistributionTalos,
			expected:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: tc.distribution,
					},
				},
			}

			assert.Equal(t, tc.expected, localregistry.ShouldSkipK3dForTest(clusterCfg))
		})
	}
}

// TestIsCloudProvider verifies cloud provider detection.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestIsCloudProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider v1alpha1.Provider
		expected bool
	}{
		{
			name:     "Docker is not cloud",
			provider: v1alpha1.ProviderDocker,
			expected: false,
		},
		{
			name:     "Hetzner is cloud",
			provider: v1alpha1.ProviderHetzner,
			expected: true,
		},
		{
			name:     "Omni is cloud",
			provider: v1alpha1.ProviderOmni,
			expected: true,
		},
		{
			name:     "empty is not cloud",
			provider: "",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Provider: tc.provider,
					},
				},
			}

			assert.Equal(t, tc.expected, localregistry.IsCloudProviderForTest(clusterCfg))
		})
	}
}

// TestNewCreateOptions verifies create options are built correctly.
func TestNewCreateOptions(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
			},
		},
	}

	opts := localregistry.NewCreateOptionsForTest(clusterCfg, "my-cluster", "kind")

	assert.Equal(t, registry.BuildLocalRegistryName("my-cluster"), opts.Name)
	assert.Equal(t, registry.DefaultEndpointHost, opts.Host)
	assert.Equal(t, "my-cluster", opts.ClusterName)
	assert.Equal(t, registry.LocalRegistryBaseName, opts.VolumeName)
}

// TestBuildVerifyOptions verifies verify options are built correctly.
func TestBuildVerifyOptions(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				LocalRegistry: v1alpha1.LocalRegistry{Registry: "ghcr.io/myorg"},
			},
		},
	}

	opts := localregistry.BuildVerifyOptionsForTest(clusterCfg)

	assert.Equal(t, "ghcr.io", opts.RegistryEndpoint)
	assert.NotEmpty(t, opts.Repository)
	assert.False(t, opts.Insecure)
}

// TestVerifyRegistryAccess_LocalRegistrySkips verifies that local registries
// are skipped (no auth required).
func TestVerifyRegistryAccess_LocalRegistrySkips(t *testing.T) {
	t.Parallel()

	t.Run("disabled registry", func(t *testing.T) {
		t.Parallel()

		clusterCfg := &v1alpha1.Cluster{}

		err := localregistry.VerifyRegistryAccess(
			newTestCmd(),
			clusterCfg,
			stubLifecycleDeps(),
		)

		require.NoError(t, err)
	})

	t.Run("local Docker registry (non-external)", func(t *testing.T) {
		t.Parallel()

		clusterCfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					LocalRegistry: v1alpha1.LocalRegistry{Registry: "localhost:5000"},
				},
			},
		}

		err := localregistry.VerifyRegistryAccess(
			newTestCmd(),
			clusterCfg,
			stubLifecycleDeps(),
		)

		require.NoError(t, err)
	})
}

// TestExecuteStage_UnsupportedStage verifies that ExecuteStage returns an error
// for unsupported stage types.
func TestExecuteStage_UnsupportedStage(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{},
	}

	err := localregistry.ExecuteStage(
		newTestCmd(),
		ctx,
		stubLifecycleDeps(),
		localregistry.StageType(99),
		localregistry.DefaultDependencies(),
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, localregistry.ErrUnsupportedStage)
}
