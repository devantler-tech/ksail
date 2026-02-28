package k3dprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
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

func TestParseClusterNodes_EmptyAndInvalid(t *testing.T) {
	t.Parallel()

	t.Run("empty output", func(t *testing.T) {
		t.Parallel()

		nodes, err := k3dprovisioner.ParseClusterNodesForTest("", "test")
		require.NoError(t, err)
		assert.Empty(t, nodes)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()

		_, err := k3dprovisioner.ParseClusterNodesForTest("not json", "test")
		require.Error(t, err)
	})

	t.Run("empty array", func(t *testing.T) {
		t.Parallel()

		nodes, err := k3dprovisioner.ParseClusterNodesForTest(`[]`, "test")
		require.NoError(t, err)
		assert.Empty(t, nodes)
	})
}

func TestParseClusterNodes_NodeCounting(t *testing.T) {
	t.Parallel()

	t.Run("single server node", func(t *testing.T) {
		t.Parallel()

		nodes, err := k3dprovisioner.ParseClusterNodesForTest(
			`[{"name":"k3d-mycluster-server-0","role":"server","labels":{}}]`,
			"mycluster",
		)
		require.NoError(t, err)
		require.Len(t, nodes, 1)
		assert.Equal(t, "server", nodes[0].Role)
	})

	t.Run("server and agents", func(t *testing.T) {
		t.Parallel()

		jsonOutput := `[
			{"name":"k3d-mycluster-server-0","role":"server","labels":{}},
			{"name":"k3d-mycluster-agent-0","role":"agent","labels":{}},
			{"name":"k3d-mycluster-agent-1","role":"agent","labels":{}}
		]`

		nodes, err := k3dprovisioner.ParseClusterNodesForTest(jsonOutput, "mycluster")
		require.NoError(t, err)
		assert.Len(t, nodes, 3)
	})
}

func TestParseClusterNodes_Filtering(t *testing.T) {
	t.Parallel()

	t.Run("filters by cluster name", func(t *testing.T) {
		t.Parallel()

		jsonOutput := `[
			{"name":"k3d-cluster-a-server-0","role":"server","labels":{}},
			{"name":"k3d-cluster-b-server-0","role":"server","labels":{}},
			{"name":"k3d-cluster-a-agent-0","role":"agent","labels":{}}
		]`

		nodes, err := k3dprovisioner.ParseClusterNodesForTest(jsonOutput, "cluster-a")
		require.NoError(t, err)
		assert.Len(t, nodes, 2)
	})

	t.Run("no matching nodes", func(t *testing.T) {
		t.Parallel()

		nodes, err := k3dprovisioner.ParseClusterNodesForTest(
			`[{"name":"k3d-other-server-0","role":"server","labels":{}}]`,
			"mycluster",
		)
		require.NoError(t, err)
		assert.Empty(t, nodes)
	})

	t.Run("node names with leading slash", func(t *testing.T) {
		t.Parallel()

		jsonOutput := `[
			{"name":"/k3d-mycluster-server-0","role":"server","labels":{}},
			{"name":"/k3d-mycluster-agent-0","role":"agent","labels":{}}
		]`

		nodes, err := k3dprovisioner.ParseClusterNodesForTest(jsonOutput, "mycluster")
		require.NoError(t, err)
		assert.Len(t, nodes, 2)
	})
}

func TestProvisioner_GetCurrentConfig_NoDetector(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewProvisioner(nil, "")
	ctx := context.Background()

	spec, err := provisioner.GetCurrentConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, spec)

	assert.Equal(t, v1alpha1.DistributionK3s, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, spec.Provider)
}

func TestProvisioner_DiffConfig_NilSimpleConfig_PreservesConfig(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Servers: 3,
		Agents:  5,
		Image:   "rancher/k3s:v1.30.0-k3s1",
	}

	provisioner := k3dprovisioner.NewProvisioner(simpleCfg, "/tmp/cfg.yaml")

	assert.Equal(t, simpleCfg, provisioner.ExportSimpleCfg())
	assert.Equal(t, "/tmp/cfg.yaml", provisioner.ExportConfigPath())
	assert.Equal(t, 3, provisioner.ExportSimpleCfg().Servers)
	assert.Equal(t, 5, provisioner.ExportSimpleCfg().Agents)
	assert.Equal(t, "rancher/k3s:v1.30.0-k3s1", provisioner.ExportSimpleCfg().Image)
}
