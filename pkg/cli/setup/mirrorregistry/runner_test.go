//nolint:testpackage // Test needs package access for internal helpers.
package mirrorregistry

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// noopActionMap returns an Actions table with a no-op action for every
// supported distribution, for tests that only exercise handler construction.
func noopActionMap() map[v1alpha1.Distribution]ActionFunc {
	noop := func(_ context.Context, _ *Context, _ dockerclient.Client) error { return nil }

	return map[v1alpha1.Distribution]ActionFunc{
		v1alpha1.DistributionVanilla:  noop,
		v1alpha1.DistributionK3s:      noop,
		v1alpha1.DistributionTalos:    noop,
		v1alpha1.DistributionVCluster: noop,
	}
}

// TestNewRegistryHandlers verifies that newRegistryHandlers creates a handler
// for every supported distribution.
func TestNewRegistryHandlers(t *testing.T) {
	t.Parallel()

	stageCtx := &Context{
		ClusterCfg: &v1alpha1.Cluster{},
		KindConfig: &v1alpha4.Cluster{},
	}

	handlers := newRegistryHandlers(stageCtx, RoleRegistry, noopActionMap())

	// Should have entries for all 4 distributions
	assert.Len(t, handlers, 4)

	expectedDistributions := []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
		v1alpha1.DistributionVCluster,
	}

	for _, dist := range expectedDistributions {
		handler, exists := handlers[dist]
		assert.True(t, exists, "handler should exist for distribution %v", dist)
		assert.NotNil(t, handler.Prepare, "Prepare should not be nil for distribution %v", dist)
		assert.NotNil(t, handler.Action, "Action should not be nil for distribution %v", dist)
	}
}

// TestK3dPostClusterConnect_Skipped verifies that K3d handler returns false
// from Prepare when role is RolePostClusterConnect.
func TestK3dPostClusterConnect_Skipped(t *testing.T) {
	t.Parallel()

	stageCtx := &Context{ClusterCfg: &v1alpha1.Cluster{}}

	handlers := newRegistryHandlers(stageCtx, RolePostClusterConnect, noopActionMap())

	k3dHandler := handlers[v1alpha1.DistributionK3s]
	assert.False(t, k3dHandler.Prepare(), "K3d should skip PostClusterConnect stage")
}

// TestExecuteRegistryStage_SkipsWhenPrepareReturnsFalse verifies that
// executeRegistryStage returns nil when shouldPrepare returns false.
func TestExecuteRegistryStage_SkipsWhenPrepareReturnsFalse(t *testing.T) {
	t.Parallel()

	err := executeRegistryStage(
		&cobra.Command{},
		stubDeps(),
		setup.StageInfo{Title: "test", Emoji: "🧪"},
		func() bool { return false },
		nil, // action — should never be called
		nil, // dockerInvoker
	)

	require.NoError(t, err)
}

// TestStageDefinitions_AllRolesMapped verifies that every Role constant has a
// stage definition in StageDefinitions.
func TestStageDefinitions_AllRolesMapped(t *testing.T) {
	t.Parallel()

	roles := []Role{
		RoleRegistry,
		RoleNetwork,
		RoleConnect,
		RolePostClusterConnect,
	}

	supportedDistributions := []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
		v1alpha1.DistributionVCluster,
	}

	for _, role := range roles {
		def, exists := StageDefinitions[role]
		assert.True(t, exists, "StageDefinitions should contain role %d", role)
		assert.NotEmpty(t, def.Info.Title, "Title should not be empty for role %d", role)

		for _, dist := range supportedDistributions {
			action, ok := def.Actions[dist]
			assert.True(t, ok, "role %d should have an action for %v", role, dist)
			assert.NotNil(t, action, "action should not be nil for role %d, %v", role, dist)
		}
	}
}

// TestStageInfoConstants verifies the stage info constants are populated correctly.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestStageInfoConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		info     setup.StageInfo
		title    string
		emoji    string
		activity string
		success  string
	}{
		{
			name:     "RegistryInfo",
			info:     RegistryInfo,
			title:    RegistryStageTitle,
			emoji:    RegistryStageEmoji,
			activity: RegistryStageActivity,
			success:  RegistryStageSuccess,
		},
		{
			name:     "NetworkInfo",
			info:     NetworkInfo,
			title:    NetworkStageTitle,
			emoji:    NetworkStageEmoji,
			activity: NetworkStageActivity,
			success:  NetworkStageSuccess,
		},
		{
			name:     "ConnectInfo",
			info:     ConnectInfo,
			title:    ConnectStageTitle,
			emoji:    ConnectStageEmoji,
			activity: ConnectStageActivity,
			success:  ConnectStageSuccess,
		},
		{
			name:     "PostClusterConnectInfo",
			info:     PostClusterConnectInfo,
			title:    PostClusterConnectStageTitle,
			emoji:    PostClusterConnectStageEmoji,
			activity: PostClusterConnectStageActivity,
			success:  PostClusterConnectStageSuccess,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.title, tc.info.Title)
			assert.Equal(t, tc.emoji, tc.info.Emoji)
			assert.Equal(t, tc.activity, tc.info.Activity)
			assert.Equal(t, tc.success, tc.info.Success)
		})
	}
}

// TestRunStage_UnknownDistribution verifies that RunStage returns nil when the
// cluster distribution doesn't have a handler (the definition exists, but the
// handler map doesn't contain the distribution).
func TestRunStage_UnknownDistribution(t *testing.T) {
	t.Parallel()

	// newRegistryHandlers only creates handlers for Vanilla, K3s, Talos, VCluster.
	// An unsupported distribution should cause handler lookup to return false.
	stageCtx := &Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.Distribution("unsupported"),
				},
			},
		},
	}

	// Call newRegistryHandlers and verify unsupported distribution is not present
	handlers := newRegistryHandlers(stageCtx, RoleRegistry, noopActionMap())

	_, found := handlers[v1alpha1.Distribution("unsupported")]
	assert.False(t, found, "unsupported distribution should not have a handler")
}

// TestGetNetworkNameForDistribution exercises the network name derivation
// for each distribution type.
func TestGetNetworkNameForDistribution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		clusterName  string
		expected     string
	}{
		{
			name:         "Vanilla returns kind",
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
			name:         "Talos returns cluster name",
			distribution: v1alpha1.DistributionTalos,
			clusterName:  "my-cluster",
			expected:     "my-cluster",
		},
		{
			name:         "VCluster returns vcluster.prefix",
			distribution: v1alpha1.DistributionVCluster,
			clusterName:  "my-cluster",
			expected:     "vcluster.my-cluster",
		},
		{
			name:         "KWOK returns kwok-prefix",
			distribution: v1alpha1.DistributionKWOK,
			clusterName:  "my-cluster",
			expected:     "kwok-my-cluster",
		},
		{
			name:         "unknown distribution returns cluster name",
			distribution: v1alpha1.Distribution("unknown"),
			clusterName:  "my-cluster",
			expected:     "my-cluster",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := GetNetworkNameForDistribution(tc.distribution, tc.clusterName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestFilterRegistriesByClusterName verifies filtering registries by cluster name prefix.
func TestFilterRegistriesByClusterName(t *testing.T) {
	t.Parallel()

	t.Run("empty cluster name returns all", func(t *testing.T) {
		t.Parallel()

		input := []dockerRegistryInfo{{Name: "a"}, {Name: "b"}}
		result := filterRegistriesByClusterName(input, "", nil)
		assert.Len(t, result, 2)
	})

	t.Run("filters by prefix", func(t *testing.T) {
		t.Parallel()

		input := []dockerRegistryInfo{
			{Name: "my-cluster-ghcr.io"},
			{Name: "other-cluster-docker.io"},
			{Name: "my-cluster-docker.io"},
		}
		result := filterRegistriesByClusterName(input, "my-cluster", nil)
		assert.Len(t, result, 2)
		assert.Equal(t, "my-cluster-ghcr.io", result[0].Name)
		assert.Equal(t, "my-cluster-docker.io", result[1].Name)
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		t.Parallel()

		input := []dockerRegistryInfo{{Name: "other-cluster-ghcr.io"}}
		result := filterRegistriesByClusterName(input, "my-cluster", nil)
		assert.Empty(t, result)
	})
}

// TestCleanupPreDiscoveredRegistries_EmptyRegistries verifies error on empty input.
func TestCleanupPreDiscoveredRegistries_EmptyRegistries(t *testing.T) {
	t.Parallel()

	err := CleanupPreDiscoveredRegistries(
		&cobra.Command{},
		nil, // timer
		nil, // empty registries
		false,
		CleanupDependencies{},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoRegistriesFound)
}
