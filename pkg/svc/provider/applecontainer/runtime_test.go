package applecontainer_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/applecontainer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runtimeTestEnv opts in to the test below, which needs Apple's container runtime and so cannot
// run on Linux CI. Set it to a Talos image reference, for example ghcr.io/siderolabs/talos:v1.14.2.
const runtimeTestEnv = "KSAIL_APPLE_CONTAINER_TEST_IMAGE"

// TestRealRuntime drives a real Talos node container through the provider's whole lifecycle.
// It creates one uniquely named cluster and removes it again, even on failure.
func TestRealRuntime(t *testing.T) {
	t.Parallel()

	image := os.Getenv(runtimeTestEnv)
	if image == "" {
		t.Skipf("set %s to run against Apple's container runtime", runtimeTestEnv)
	}

	prov := applecontainer.NewDefaultProvider()
	require.NoError(t, prov.CheckAvailable(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	cluster := fmt.Sprintf("ksail-test-%d", time.Now().UnixNano())
	node := cluster + "-control-plane-1"

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()

		assert.NoError(t, prov.DeleteNodes(cleanupCtx, cluster))
	})

	require.NoError(t, prov.CreateNode(ctx, talosNodeSpec(node, cluster, image)))

	// A second node with the same name is refused and leaves the first one alone.
	err := prov.CreateNode(ctx, applecontainer.NodeSpec{
		Name: node, ClusterName: cluster, Role: applecontainer.RoleWorker, Image: image,
	})
	require.ErrorIs(t, err, applecontainer.ErrNodeExists)

	nodes, err := prov.ListNodes(ctx, cluster)
	require.NoError(t, err)
	assert.Equal(t, []provider.NodeInfo{{
		Name: node, ClusterName: cluster,
		Role: applecontainer.RoleControlPlane, State: applecontainer.StateRunning,
	}}, nodes)

	clusters, err := prov.ListAllClusters(ctx)
	require.NoError(t, err)
	assert.Contains(t, clusters, cluster)

	assertRestartCycle(ctx, t, prov, cluster, node)

	require.NoError(t, prov.DeleteNodes(ctx, cluster))

	exists, err := prov.NodesExist(ctx, cluster)
	require.NoError(t, err)
	assert.False(t, exists)

	volumes, err := applecontainer.NewExecRunner("").
		Run(ctx, "volume", "list", "--format", "json")
	require.NoError(t, err)
	assert.NotContains(t, string(volumes), cluster)
}

// talosNodeSpec returns a control-plane node with the flags Talos needs on this runtime, from the
// trial recorded on ksail#7577.
func talosNodeSpec(node, cluster, image string) applecontainer.NodeSpec {
	return applecontainer.NodeSpec{
		Name:           node,
		ClusterName:    cluster,
		Role:           applecontainer.RoleControlPlane,
		Image:          image,
		CPUs:           2,
		MemoryMiB:      2048,
		Privileged:     true,
		ReadOnlyRootFS: true,
		Env:            map[string]string{"PLATFORM": "container"},
		Tmpfs: []string{
			"/run", "/system", "/tmp", "/etc/cni", "/etc/kubernetes", "/usr/libexec/kubernetes",
		},
		Volumes: []applecontainer.VolumeMount{
			{Name: "state", Destination: "/system/state", Size: "1G"},
			{Name: "var", Destination: "/var", Size: "4G"},
		},
	}
}

// assertRestartCycle stops and starts the cluster and checks what the provider reports in between.
func assertRestartCycle(
	ctx context.Context,
	t *testing.T,
	prov *applecontainer.Provider,
	cluster, node string,
) {
	t.Helper()

	first, err := prov.NodeAddress(ctx, cluster, node)
	require.NoError(t, err)
	assert.True(t, first.Is4())

	require.NoError(t, prov.StopNodes(ctx, cluster))

	status, err := prov.GetClusterStatus(ctx, cluster)
	require.NoError(t, err)
	assert.Equal(t, provider.PhaseStopped, status.Phase)

	_, err = prov.NodeAddress(ctx, cluster, node)
	require.ErrorIs(t, err, applecontainer.ErrNoAddress)

	require.NoError(t, prov.StartNodes(ctx, cluster))

	status, err = prov.GetClusterStatus(ctx, cluster)
	require.NoError(t, err)
	assert.True(t, status.Ready)

	second, err := prov.NodeAddress(ctx, cluster, node)
	require.NoError(t, err)
	t.Logf("node address before stop: %s, after start: %s", first, second)
}
