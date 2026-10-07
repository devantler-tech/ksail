package applecontainer_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/applecontainer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time proof that the provider satisfies the shared interfaces.
var (
	_ provider.Provider          = (*applecontainer.Provider)(nil)
	_ provider.AvailableProvider = (*applecontainer.Provider)(nil)
)

var errFake = errors.New("fake CLI failure")

// containersJSON mirrors what `container list --all --format json` printed on CLI 1.5.0: two
// nodes of cluster "dev" (one stopped, and so without networks), one node of cluster "other",
// the runtime's own builder container, and a foreign container that only shares the cluster label.
const containersJSON = `[
 {"id":"dev-worker-1","configuration":{"id":"dev-worker-1","labels":{
   "ksail.io/managed-by":"ksail","ksail.io/cluster":"dev","ksail.io/role":"worker"}},
  "status":{"state":"stopped","networks":[]}},
 {"id":"dev-control-plane-1","configuration":{"id":"dev-control-plane-1","labels":{
   "ksail.io/managed-by":"ksail","ksail.io/cluster":"dev","ksail.io/role":"control-plane"}},
  "status":{"state":"running","startedDate":"2026-10-07T00:00:00Z","networks":[
   {"hostname":"dev-control-plane-1","ipv4Address":"192.168.64.23/24",
    "ipv6Address":"fdba:bf0f:45ca:a695::1/64","network":"default"}]}},
 {"id":"other-control-plane-1","configuration":{"labels":{
   "ksail.io/managed-by":"ksail","ksail.io/cluster":"other","ksail.io/role":"control-plane"}},
  "status":{"state":"running","networks":[{"ipv4Address":"192.168.64.24/24"}]}},
 {"id":"buildkit","configuration":{"labels":{"com.apple.container.plugin":"builder"}},
  "status":{"state":"running","networks":[{"ipv4Address":"192.168.64.2/24"}]}},
 {"id":"foreign","configuration":{"labels":{"ksail.io/cluster":"dev"}},
  "status":{"state":"running","networks":[]}}
]`

// volumesJSON mirrors `container volume list --format json`: two volumes of cluster "dev", one
// of cluster "other", and an unlabelled volume whose name merely looks like a "dev" volume.
const volumesJSON = `[
 {"id":"dev-control-plane-1-state","configuration":{"name":"dev-control-plane-1-state","labels":{
   "ksail.io/managed-by":"ksail","ksail.io/cluster":"dev","ksail.io/role":"control-plane"}}},
 {"id":"dev-control-plane-1-var","configuration":{"labels":{
   "ksail.io/managed-by":"ksail","ksail.io/cluster":"dev","ksail.io/role":"control-plane"}}},
 {"id":"other-control-plane-1-state","configuration":{"labels":{
   "ksail.io/managed-by":"ksail","ksail.io/cluster":"other"}}},
 {"id":"dev-lookalike","configuration":{"labels":{}}}
]`

// fakeCLI is a Runner that answers from canned output and records every invocation.
type fakeCLI struct {
	mu sync.Mutex
	// calls holds each invocation as its space-joined arguments.
	calls []string
	// containers and volumes are the JSON printed for the two list subcommands.
	containers string
	volumes    string
	// failPrefix makes every invocation whose joined arguments start with it fail with failErr.
	failPrefix string
	failErr    error
}

func newFakeCLI() *fakeCLI {
	return &fakeCLI{containers: containersJSON, volumes: volumesJSON, failErr: errFake}
}

func (f *fakeCLI) Run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	joined := strings.Join(args, " ")
	f.calls = append(f.calls, joined)

	if f.failPrefix != "" && strings.HasPrefix(joined, f.failPrefix) {
		return nil, f.failErr
	}

	switch joined {
	case "list --all --format json":
		return []byte(f.containers), nil
	case "volume list --format json":
		return []byte(f.volumes), nil
	default:
		return nil, nil
	}
}

// mutations returns the recorded invocations that change state (everything but the two lists).
func (f *fakeCLI) mutations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var result []string

	for _, call := range f.calls {
		if !strings.Contains(call, "list") && call != "system status" {
			result = append(result, call)
		}
	}

	return result
}

func TestNilRunnerIsUnavailable(t *testing.T) {
	t.Parallel()

	prov := applecontainer.NewProvider(nil)
	ctx := context.Background()

	assert.False(t, prov.IsAvailable())
	require.ErrorIs(t, prov.CheckAvailable(ctx), provider.ErrProviderUnavailable)

	_, err := prov.ListNodes(ctx, "dev")
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)

	_, err = prov.ListAllClusters(ctx)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)

	_, err = prov.NodesExist(ctx, "dev")
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)

	require.ErrorIs(t, prov.StartNodes(ctx, "dev"), provider.ErrProviderUnavailable)
	require.ErrorIs(t, prov.StopNodes(ctx, "dev"), provider.ErrProviderUnavailable)
	require.ErrorIs(t, prov.DeleteNodes(ctx, "dev"), provider.ErrProviderUnavailable)

	_, err = prov.GetClusterStatus(ctx, "dev")
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)

	err = prov.CreateNode(ctx, applecontainer.NodeSpec{})
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestCheckAvailable(t *testing.T) {
	t.Parallel()

	t.Run("ServiceAnswers", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		prov := applecontainer.NewProvider(cli)

		require.NoError(t, prov.CheckAvailable(context.Background()))
		assert.True(t, prov.IsAvailable())
		assert.Contains(t, cli.calls, "system status")
	})

	t.Run("CLIMissing", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.failPrefix = "system status"
		cli.failErr = applecontainer.ErrCLINotFound
		prov := applecontainer.NewProvider(cli)

		err := prov.CheckAvailable(context.Background())

		require.ErrorIs(t, err, applecontainer.ErrCLINotFound)
		require.NotErrorIs(t, err, applecontainer.ErrServiceNotRunning)
		assert.Contains(t, err.Error(), "install it from")
		assert.False(t, prov.IsAvailable())
	})

	t.Run("ServiceStopped", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.failPrefix = "system status"
		prov := applecontainer.NewProvider(cli)

		err := prov.CheckAvailable(context.Background())

		require.ErrorIs(t, err, applecontainer.ErrServiceNotRunning)
		assert.Contains(t, err.Error(), "container system start")
		assert.False(t, prov.IsAvailable())
	})
}

func TestListNodes(t *testing.T) {
	t.Parallel()

	prov := applecontainer.NewProvider(newFakeCLI())

	nodes, err := prov.ListNodes(context.Background(), "dev")

	require.NoError(t, err)
	// Sorted by name; the unlabelled "foreign" container and the other cluster's node are absent.
	assert.Equal(t, []provider.NodeInfo{
		{
			Name: "dev-control-plane-1", ClusterName: "dev",
			Role: applecontainer.RoleControlPlane, State: applecontainer.StateRunning,
		},
		{
			Name: "dev-worker-1", ClusterName: "dev",
			Role: applecontainer.RoleWorker, State: "stopped",
		},
	}, nodes)
}

func TestListNodes_UnknownClusterIsEmpty(t *testing.T) {
	t.Parallel()

	prov := applecontainer.NewProvider(newFakeCLI())
	ctx := context.Background()

	nodes, err := prov.ListNodes(ctx, "missing")
	require.NoError(t, err)
	assert.Empty(t, nodes)

	exists, err := prov.NodesExist(ctx, "missing")
	require.NoError(t, err)
	assert.False(t, exists)

	exists, err = prov.NodesExist(ctx, "dev")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestListNodes_Failures(t *testing.T) {
	t.Parallel()

	t.Run("CLIFails", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.failPrefix = "list"

		_, err := applecontainer.NewProvider(cli).ListNodes(context.Background(), "dev")

		require.ErrorIs(t, err, errFake)
	})

	t.Run("OutputIsNotJSON", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.containers = "ID  IMAGE  STATE\n"

		_, err := applecontainer.NewProvider(cli).ListNodes(context.Background(), "dev")

		require.ErrorIs(t, err, applecontainer.ErrInvalidOutput)
	})
}

func TestListAllClusters(t *testing.T) {
	t.Parallel()

	prov := applecontainer.NewProvider(newFakeCLI())

	clusters, err := prov.ListAllClusters(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"dev", "other"}, clusters)
}

func TestStartNodes(t *testing.T) {
	t.Parallel()

	t.Run("StartsOnlyStoppedNodes", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()

		require.NoError(t, applecontainer.NewProvider(cli).StartNodes(context.Background(), "dev"))
		assert.Equal(t, []string{"start -- dev-worker-1"}, cli.mutations())
	})

	t.Run("NoNodes", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()

		err := applecontainer.NewProvider(cli).StartNodes(context.Background(), "missing")

		require.ErrorIs(t, err, provider.ErrNoNodes)
		assert.Empty(t, cli.mutations())
	})

	t.Run("CLIFails", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.failPrefix = "start"

		err := applecontainer.NewProvider(cli).StartNodes(context.Background(), "dev")

		require.ErrorIs(t, err, errFake)
		assert.Contains(t, err.Error(), "dev-worker-1")
	})
}

func TestStopNodes(t *testing.T) {
	t.Parallel()

	t.Run("StopsOnlyRunningNodes", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()

		require.NoError(t, applecontainer.NewProvider(cli).StopNodes(context.Background(), "dev"))
		assert.Equal(t, []string{"stop -- dev-control-plane-1"}, cli.mutations())
	})

	t.Run("NothingRunning", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.containers = strings.ReplaceAll(containersJSON, `"running"`, `"stopped"`)

		require.NoError(t, applecontainer.NewProvider(cli).StopNodes(context.Background(), "dev"))
		assert.Empty(t, cli.mutations())
	})

	t.Run("NoNodes", func(t *testing.T) {
		t.Parallel()

		err := applecontainer.NewProvider(newFakeCLI()).StopNodes(context.Background(), "missing")

		require.ErrorIs(t, err, provider.ErrNoNodes)
	})

	t.Run("CLIFails", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.failPrefix = "stop"

		err := applecontainer.NewProvider(cli).StopNodes(context.Background(), "dev")

		require.ErrorIs(t, err, errFake)
	})
}

func TestDeleteNodes(t *testing.T) {
	t.Parallel()

	t.Run("RemovesContainersThenLabelledVolumes", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()

		require.NoError(t, applecontainer.NewProvider(cli).DeleteNodes(context.Background(), "dev"))
		// Neither the other cluster's resources, the foreign container nor the unlabelled
		// look-alike volume are touched.
		assert.Equal(t, []string{
			"delete --force -- dev-control-plane-1 dev-worker-1",
			"volume delete -- dev-control-plane-1-state dev-control-plane-1-var",
		}, cli.mutations())
	})

	t.Run("RemovesLeftoverVolumesWithoutContainers", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.containers = "[]"

		require.NoError(t, applecontainer.NewProvider(cli).DeleteNodes(context.Background(), "dev"))
		assert.Equal(t, []string{
			"volume delete -- dev-control-plane-1-state dev-control-plane-1-var",
		}, cli.mutations())
	})

	t.Run("NothingToDelete", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()

		err := applecontainer.NewProvider(cli).DeleteNodes(context.Background(), "missing")

		require.NoError(t, err)
		assert.Empty(t, cli.mutations())
	})

	t.Run("ContainerDeleteFailsKeepsVolumes", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.failPrefix = "delete"

		err := applecontainer.NewProvider(cli).DeleteNodes(context.Background(), "dev")

		require.ErrorIs(t, err, errFake)
		assert.Equal(t,
			[]string{"delete --force -- dev-control-plane-1 dev-worker-1"}, cli.mutations())
	})

	t.Run("VolumeOutputIsNotJSON", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.volumes = "NAME\n"

		err := applecontainer.NewProvider(cli).DeleteNodes(context.Background(), "dev")

		require.ErrorIs(t, err, applecontainer.ErrInvalidOutput)
	})
}

func TestGetClusterStatus(t *testing.T) {
	t.Parallel()

	prov := applecontainer.NewProvider(newFakeCLI())
	ctx := context.Background()

	status, err := prov.GetClusterStatus(ctx, "dev")
	require.NoError(t, err)
	assert.Equal(t, provider.PhaseDegraded, status.Phase)
	assert.False(t, status.Ready)
	assert.Equal(t, 2, status.NodesTotal)
	assert.Equal(t, 1, status.NodesReady)

	status, err = prov.GetClusterStatus(ctx, "other")
	require.NoError(t, err)
	assert.Equal(t, applecontainer.StateRunning, status.Phase)
	assert.True(t, status.Ready)

	_, err = prov.GetClusterStatus(ctx, "missing")
	require.ErrorIs(t, err, provider.ErrClusterNotFound)
}

func TestNodeAddress(t *testing.T) {
	t.Parallel()

	prov := applecontainer.NewProvider(newFakeCLI())
	ctx := context.Background()

	addr, err := prov.NodeAddress(ctx, "dev", "dev-control-plane-1")
	require.NoError(t, err)
	assert.Equal(t, netip.MustParseAddr("192.168.64.23"), addr)

	// A stopped node has no address.
	_, err = prov.NodeAddress(ctx, "dev", "dev-worker-1")
	require.ErrorIs(t, err, applecontainer.ErrNoAddress)

	// A node is only found inside its own cluster, and never among unmanaged containers.
	_, err = prov.NodeAddress(ctx, "dev", "other-control-plane-1")
	require.ErrorIs(t, err, applecontainer.ErrNodeNotFound)

	_, err = prov.NodeAddress(ctx, "dev", "foreign")
	require.ErrorIs(t, err, applecontainer.ErrNodeNotFound)
}
