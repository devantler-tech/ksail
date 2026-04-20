package omni_test

import (
	"context"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/siderolabs/omni/client/api/omni/specs"
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

func TestWaitForClusterRunning_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	err := prov.WaitForClusterRunning(context.Background(), "test-cluster", time.Second)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestWaitForClusterRunning_ClusterNotFound_TimesOut(t *testing.T) {
	t.Parallel()

	prov := omni.NewProviderWithState(newInMemState())

	ctx := context.Background()
	err := prov.WaitForClusterRunning(ctx, "nonexistent", 500*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestWaitForClusterRunning_CancelledContext(t *testing.T) {
	t.Parallel()

	prov := omni.NewProviderWithState(newInMemState())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := prov.WaitForClusterRunning(ctx, "test-cluster", 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestWaitForClusterRunning_RunningNotReady_Succeeds(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	// Create a ClusterStatus with Phase=RUNNING but Ready=false
	// This simulates a cluster where CNI hasn't been installed yet
	cs := omnires.NewClusterStatus("test-cluster")
	cs.TypedSpec().Value.Phase = specs.ClusterStatusSpec_RUNNING
	cs.TypedSpec().Value.Ready = false

	require.NoError(t, testState.Create(context.Background(), cs))

	err := prov.WaitForClusterRunning(context.Background(), "test-cluster", 5*time.Second)

	// Should succeed because Phase==RUNNING, even though Ready==false
	require.NoError(t, err)
}

func TestWaitForClusterRunning_RunningAndReady_Succeeds(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	cs := omnires.NewClusterStatus("test-cluster")
	cs.TypedSpec().Value.Phase = specs.ClusterStatusSpec_RUNNING
	cs.TypedSpec().Value.Ready = true

	require.NoError(t, testState.Create(context.Background(), cs))

	err := prov.WaitForClusterRunning(context.Background(), "test-cluster", 5*time.Second)

	require.NoError(t, err)
}

func TestWaitForClusterReady_RunningNotReady_TimesOut(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	// Create a ClusterStatus with Phase=RUNNING but Ready=false
	cs := omnires.NewClusterStatus("test-cluster")
	cs.TypedSpec().Value.Phase = specs.ClusterStatusSpec_RUNNING
	cs.TypedSpec().Value.Ready = false

	require.NoError(t, testState.Create(context.Background(), cs))

	err := prov.WaitForClusterReady(context.Background(), "test-cluster", 500*time.Millisecond)

	// Should time out because Ready==false
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestWaitForClusterReady_RunningAndReady_Succeeds(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	cs := omnires.NewClusterStatus("test-cluster")
	cs.TypedSpec().Value.Phase = specs.ClusterStatusSpec_RUNNING
	cs.TypedSpec().Value.Ready = true

	require.NoError(t, testState.Create(context.Background(), cs))

	err := prov.WaitForClusterReady(context.Background(), "test-cluster", 5*time.Second)

	require.NoError(t, err)
}

func TestWaitForClusterReady_NotFoundTimesOut(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	err := prov.WaitForClusterReady(context.Background(), "nonexistent", 500*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestWaitForClusterReady_CancelledContext(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := prov.WaitForClusterReady(ctx, "test-cluster", 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestGetKubeconfig_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	data, err := prov.GetKubeconfig(context.Background(), "test-cluster", 0)

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

func TestDeleteNodes_ClusterNotFound_ReturnsNil(t *testing.T) {
	t.Parallel()

	prov := omni.NewProviderWithState(newInMemState())

	// The cluster does not exist in state; DeleteNodes must treat NotFound as success.
	err := prov.DeleteNodes(context.Background(), "nonexistent-cluster")

	require.NoError(t, err)
}

func TestLatestTalosVersion_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	_, _, err := prov.LatestTalosVersion(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestLatestTalosVersion_NoVersions(t *testing.T) {
	t.Parallel()

	prov := omni.NewProviderWithState(newInMemState())

	_, _, err := prov.LatestTalosVersion(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, omni.ErrNoTalosVersions)
}

func TestLatestTalosVersion_ReturnsLatest(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	// Seed multiple versions
	for _, v := range []string{"1.11.2", "1.12.4", "1.10.0"} {
		tv := omnires.NewTalosVersion(v)
		tv.TypedSpec().Value.CompatibleKubernetesVersions = []string{"1.31.0", "1.32.0"}

		require.NoError(t, testState.Create(context.Background(), tv))
	}

	version, k8sVersions, err := prov.LatestTalosVersion(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "1.12.4", version)
	assert.Equal(t, []string{"1.31.0", "1.32.0"}, k8sVersions)
}

func TestLatestTalosVersion_SkipsDeprecated(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	// Add a higher version that is deprecated
	tvOld := omnires.NewTalosVersion("1.11.2")
	tvOld.TypedSpec().Value.CompatibleKubernetesVersions = []string{"1.31.0"}

	require.NoError(t, testState.Create(context.Background(), tvOld))

	tvNew := omnires.NewTalosVersion("1.12.4")
	tvNew.TypedSpec().Value.Deprecated = true
	tvNew.TypedSpec().Value.CompatibleKubernetesVersions = []string{"1.32.0"}

	require.NoError(t, testState.Create(context.Background(), tvNew))

	version, k8sVersions, err := prov.LatestTalosVersion(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "1.11.2", version)
	assert.Equal(t, []string{"1.31.0"}, k8sVersions)
}

func TestDeleteNodes_ClusterExists_RemovesCluster(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	cluster := omnires.NewCluster("test-cluster")
	require.NoError(t, testState.Create(context.Background(), cluster))

	err := prov.DeleteNodes(context.Background(), "test-cluster")

	require.NoError(t, err)

	_, getErr := testState.Get(context.Background(), cluster.Metadata())
	require.Error(t, getErr)
	assert.True(t, state.IsNotFoundError(getErr), "cluster should have been removed from state")
}

func TestListAvailableMachines_NilClient(t *testing.T) {
	t.Parallel()

	prov := omni.NewProvider(nil)

	machines, err := prov.ListAvailableMachines(context.Background(), 1)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, machines)
}

func TestListAvailableMachines_NoMachines(t *testing.T) {
	t.Parallel()

	prov := omni.NewProviderWithState(newInMemState())

	machines, err := prov.ListAvailableMachines(context.Background(), 1)

	require.Error(t, err)
	require.ErrorIs(t, err, omni.ErrInsufficientAvailableMachines)
	assert.Nil(t, machines)
}

func TestListAvailableMachines_SufficientMachines(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	// Seed 3 available machines
	for _, id := range []string{"uuid-1", "uuid-2", "uuid-3"} {
		ms := omnires.NewMachineStatus(id)
		ms.Metadata().Labels().Set(omnires.MachineStatusLabelAvailable, "")

		require.NoError(t, testState.Create(context.Background(), ms))
	}

	machines, err := prov.ListAvailableMachines(context.Background(), 2)

	require.NoError(t, err)
	assert.Len(t, machines, 2)
}

func TestListAvailableMachines_ExactCount(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	// Seed exactly 3 available machines
	for _, id := range []string{"uuid-1", "uuid-2", "uuid-3"} {
		ms := omnires.NewMachineStatus(id)
		ms.Metadata().Labels().Set(omnires.MachineStatusLabelAvailable, "")

		require.NoError(t, testState.Create(context.Background(), ms))
	}

	machines, err := prov.ListAvailableMachines(context.Background(), 3)

	require.NoError(t, err)
	assert.Len(t, machines, 3)
}

func TestListAvailableMachines_InsufficientMachines(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	// Seed only 1 available machine
	ms := omnires.NewMachineStatus("uuid-1")
	ms.Metadata().Labels().Set(omnires.MachineStatusLabelAvailable, "")

	require.NoError(t, testState.Create(context.Background(), ms))

	machines, err := prov.ListAvailableMachines(context.Background(), 3)

	require.Error(t, err)
	require.ErrorIs(t, err, omni.ErrInsufficientAvailableMachines)
	assert.Contains(t, err.Error(), "need 3")
	assert.Contains(t, err.Error(), "got 1")
	assert.Nil(t, machines)
}

func TestListAvailableMachines_SkipsUnavailable(t *testing.T) {
	t.Parallel()

	testState := newInMemState()
	prov := omni.NewProviderWithState(testState)

	// Seed 1 available and 1 unavailable machine
	available := omnires.NewMachineStatus("uuid-available")
	available.Metadata().Labels().Set(omnires.MachineStatusLabelAvailable, "")

	require.NoError(t, testState.Create(context.Background(), available))

	unavailable := omnires.NewMachineStatus("uuid-allocated")
	// No available label — this machine is already in a cluster.
	require.NoError(t, testState.Create(context.Background(), unavailable))

	machines, err := prov.ListAvailableMachines(context.Background(), 1)

	require.NoError(t, err)
	assert.Equal(t, []string{"uuid-available"}, machines)
}

func TestListAvailableMachines_NegativeCount(t *testing.T) {
	t.Parallel()

	prov := omni.NewProviderWithState(newInMemState())

	machines, err := prov.ListAvailableMachines(context.Background(), -1)

	require.Error(t, err)
	// Negative count is an input validation error, not an availability error.
	require.NotErrorIs(t, err, omni.ErrInsufficientAvailableMachines)
	require.ErrorIs(t, err, omni.ErrNegativeMachineCount)
	assert.Nil(t, machines)
}

func TestListAvailableMachines_ZeroCount(t *testing.T) {
	t.Parallel()

	prov := omni.NewProviderWithState(newInMemState())

	machines, err := prov.ListAvailableMachines(context.Background(), 0)

	require.NoError(t, err)
	assert.Empty(t, machines)
}
