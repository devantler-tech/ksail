package omni_test

import (
	"context"
	"testing"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newInMemState() state.State {
	return state.WrapCore(namespaced.NewState(inmem.Build))
}

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
	assert.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestStopNodes_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	err := prov.StopNodes(context.Background(), "test-cluster")

	require.Error(t, err)
	assert.ErrorIs(t, err, provider.ErrProviderUnavailable)
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
	assert.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestDeleteNodes_ClusterNotFound_ReturnsNil(t *testing.T) {
	t.Parallel()

	prov := omni.NewProviderWithState(newInMemState())

	// The cluster does not exist in state; DeleteNodes must treat NotFound as success.
	err := prov.DeleteNodes(context.Background(), "nonexistent-cluster")

	require.NoError(t, err)
}

func TestDeleteNodes_ClusterExists_RemovesCluster(t *testing.T) {
	t.Parallel()

	st := newInMemState()
	prov := omni.NewProviderWithState(st)

	cluster := omnires.NewCluster("test-cluster")
	require.NoError(t, st.Create(context.Background(), cluster))

	err := prov.DeleteNodes(context.Background(), "test-cluster")

	require.NoError(t, err)
	_, getErr := st.Get(context.Background(), cluster.Metadata())
	require.Error(t, getErr)
	assert.True(t, state.IsNotFoundError(getErr), "cluster should have been removed from state")
}

