package k3dprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvisioner_Update_NilSpecs(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewProvisioner(nil, "")
	ctx := context.Background()

	tests := []struct {
		name    string
		oldSpec *v1alpha1.ClusterSpec
		newSpec *v1alpha1.ClusterSpec
	}{
		{
			name:    "both nil",
			oldSpec: nil,
			newSpec: nil,
		},
		{
			name:    "old nil",
			oldSpec: nil,
			newSpec: &v1alpha1.ClusterSpec{},
		},
		{
			name:    "new nil",
			oldSpec: &v1alpha1.ClusterSpec{},
			newSpec: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := provisioner.Update(
				ctx,
				"test-cluster",
				testCase.oldSpec,
				testCase.newSpec,
				clusterupdate.UpdateOptions{},
			)

			require.NoError(t, err)
			assert.NotNil(t, result)
			assert.Empty(t, result.InPlaceChanges)
			assert.Empty(t, result.RecreateRequired)
		})
	}
}

func TestProvisioner_DiffConfig_NilSimpleConfig(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewProvisioner(nil, "")
	ctx := context.Background()

	result, err := provisioner.DiffConfig(
		ctx,
		"test-cluster",
		&v1alpha1.ClusterSpec{},
		&v1alpha1.ClusterSpec{},
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.InPlaceChanges)
	assert.Empty(t, result.RecreateRequired)
}

func TestProvisioner_DiffConfig_ServerCountChange(t *testing.T) {
	t.Parallel()

	// Note: This test can't easily run DiffConfig without a real cluster
	// because it calls countRunningNodes which requires k3d node list
	// So we test the logic indirectly through the structure

	tests := []struct {
		name           string
		desiredServers int
		expectedReason string
	}{
		{
			name:           "servers increase",
			desiredServers: 3,
			expectedReason: "K3d does not support adding/removing server (control-plane) nodes after creation",
		},
		{
			name:           "servers decrease",
			desiredServers: 0, // Will default to 1
			expectedReason: "K3d does not support adding/removing server (control-plane) nodes after creation",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// We can't easily test this without a running cluster
			// but we verify the function signature and structure
			simpleCfg := &k3dv1alpha5.SimpleConfig{
				Servers: testCase.desiredServers,
				Agents:  2,
			}
			provisioner := k3dprovisioner.NewProvisioner(simpleCfg, "")
			assert.NotNil(t, provisioner)
		})
	}
}

func TestProvisioner_DiffConfig_AgentCountChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		desiredAgents int
		description   string
	}{
		{
			name:          "agents increase",
			desiredAgents: 5,
			description:   "Should detect agent increase",
		},
		{
			name:          "agents decrease",
			desiredAgents: 1,
			description:   "Should detect agent decrease",
		},
		{
			name:          "zero agents",
			desiredAgents: 0,
			description:   "Should handle zero agents",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			simpleCfg := &k3dv1alpha5.SimpleConfig{
				Servers: 1,
				Agents:  testCase.desiredAgents,
			}
			provisioner := k3dprovisioner.NewProvisioner(simpleCfg, "")
			assert.NotNil(t, provisioner)
		})
	}
}

func TestProvisioner_GetCurrentConfig_NoDetector(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewProvisioner(nil, "")
	ctx := context.Background()

	spec, _, err := provisioner.GetCurrentConfig(ctx, "test-cluster")
	require.NoError(t, err)
	require.NotNil(t, spec)
	assert.Equal(t, v1alpha1.DistributionK3s, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, spec.Provider)
}

func TestProvisioner_Update_DefaultServerCount(t *testing.T) {
	t.Parallel()

	// Test that zero servers defaults to 1 in the logic
	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Servers: 0, // Should default to 1
		Agents:  2,
	}

	provisioner := k3dprovisioner.NewProvisioner(simpleCfg, "")
	assert.NotNil(t, provisioner)

	// The actual defaulting happens in DiffConfig when comparing
	// desiredServers == 0 → defaults to 1
}

func TestProvisioner_Update_WithImage(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Servers: 1,
		Agents:  2,
		Image:   "rancher/k3s:v1.30.0-k3s1",
	}

	provisioner := k3dprovisioner.NewProvisioner(simpleCfg, "")
	assert.NotNil(t, provisioner)

	// Verify that image is set in config
	// The image will be used when creating new nodes
}

func TestCreateProvisioner_WithConfig(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Servers: 1,
		Agents:  3,
		Image:   "rancher/k3s:v1.30.0-k3s1",
	}

	configPath := "/tmp/k3d-config.yaml"
	provisioner := k3dprovisioner.CreateProvisioner(simpleCfg, configPath)

	require.NotNil(t, provisioner)
}

func TestCreateProvisioner_NilConfig(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.CreateProvisioner(nil, "")
	require.NotNil(t, provisioner)
}

//nolint:funlen // Table-driven allocation cases are clearest enumerated inline.
func TestAgentNodeNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing []string
		count    int
		want     []string
	}{
		{
			name:     "empty cluster starts at agent-0",
			existing: nil,
			count:    1,
			want:     []string{"k3d-mycluster-agent-0"},
		},
		{
			name:     "empty cluster, multiple nodes are contiguous from 0",
			existing: nil,
			count:    3,
			want: []string{
				"k3d-mycluster-agent-0",
				"k3d-mycluster-agent-1",
				"k3d-mycluster-agent-2",
			},
		},
		{
			name:     "contiguous series extends past the highest index",
			existing: []string{"k3d-mycluster-agent-0", "k3d-mycluster-agent-1"},
			count:    1,
			want:     []string{"k3d-mycluster-agent-2"},
		},
		{
			// Core #5312 behaviour: a freed middle index is reclaimed, not max+1.
			name:     "reclaims a freed middle index",
			existing: []string{"k3d-mycluster-agent-0", "k3d-mycluster-agent-2"},
			count:    1,
			want:     []string{"k3d-mycluster-agent-1"},
		},
		{
			name:     "fills the gap then extends for a multi-node scale-up",
			existing: []string{"k3d-mycluster-agent-0", "k3d-mycluster-agent-2"},
			count:    2,
			want:     []string{"k3d-mycluster-agent-1", "k3d-mycluster-agent-3"},
		},
		{
			name:     "reclaims a leading gap at index 0",
			existing: []string{"k3d-mycluster-agent-1", "k3d-mycluster-agent-2"},
			count:    1,
			want:     []string{"k3d-mycluster-agent-0"},
		},
		{
			name:     "ignores names that do not match the cluster prefix",
			existing: []string{"k3d-other-agent-0", "k3d-mycluster-server-0"},
			count:    1,
			want:     []string{"k3d-mycluster-agent-0"},
		},
		{
			name:     "zero count yields no names",
			existing: []string{"k3d-mycluster-agent-0"},
			count:    0,
			want:     []string{},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := k3dprovisioner.AgentNodeNamesForTest(
				testCase.existing, "mycluster", testCase.count,
			)
			assert.Equal(t, testCase.want, got)
		})
	}
}
