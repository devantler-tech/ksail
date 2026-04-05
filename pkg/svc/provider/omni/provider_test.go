package omni_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProvider(t *testing.T) {
	t.Parallel()

	t.Run("WithNilClient", func(t *testing.T) {
		t.Parallel()

		prov := omni.NewProvider(nil)

		require.NotNil(t, prov)
		assert.False(t, prov.IsAvailable())
	})
}

func TestIsAvailable(t *testing.T) {
	t.Parallel()

	t.Run("WithNilClient", func(t *testing.T) {
		t.Parallel()

		prov := omni.NewProvider(nil)

		assert.False(t, prov.IsAvailable())
	})
}

func TestStartNodes_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	err := prov.StartNodes(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestStopNodes_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	err := prov.StopNodes(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestListNodes_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	nodes, err := prov.ListNodes(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, nodes)
}

func TestListAllClusters_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	clusters, err := prov.ListAllClusters(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, clusters)
}

func TestNodesExist_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	exists, err := prov.NodesExist(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.False(t, exists)
}

func TestDeleteNodes_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	err := prov.DeleteNodes(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestClusterExists_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	exists, err := prov.ClusterExists(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.False(t, exists)
}

func TestCreateCluster_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	err := prov.CreateCluster(context.Background(), nil, nil)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestWaitForClusterReady_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	err := prov.WaitForClusterReady(context.Background(), "test-cluster", time.Second)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestGetKubeconfig_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	data, err := prov.GetKubeconfig(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, data)
}

func TestGetTalosconfig_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	data, err := prov.GetTalosconfig(context.Background(), "test-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, data)
}

func TestClient_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	assert.Nil(t, prov.Client())
}
