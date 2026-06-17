package talosprovisioner_test

import (
	"context"
	"testing"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRolling_SortNodesWorkersFirst(t *testing.T) { //nolint:funlen // table-driven tests
	t.Parallel()

	tests := []struct {
		name     string
		nodes    []talosprovisioner.NodeWithRoleForTest
		expected []talosprovisioner.NodeWithRoleForTest
	}{
		{
			name:     "empty list",
			nodes:    nil,
			expected: []talosprovisioner.NodeWithRoleForTest{},
		},
		{
			name: "workers before control-planes",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.4", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.5", "worker"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.4", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.5", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
			},
		},
		{
			name: "only control-planes",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
			},
		},
		{
			name: "only workers",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.6", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.4", "worker"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.4", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.6", "worker"),
			},
		},
		{
			name: "single node",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
			},
		},
		{
			name: "IPs sorted within groups",
			nodes: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.9", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.7", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.8", "worker"),
			},
			expected: []talosprovisioner.NodeWithRoleForTest{
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.7", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.8", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.9", "worker"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.2", "control-plane"),
				talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", "control-plane"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := talosprovisioner.SortNodesWorkersFirstForTest(tt.nodes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRolling_BuildStagedNodeConfig_PreservesNodeHostname is the regression guard
// for the rolling-reboot node-rename bug — the sibling of the in-place
// TestBuildDesiredNodeConfig_PreservesNodeHostname. The config a rolling reboot
// STAGES (applied on the node's next reboot) must be rebuilt through
// buildDesiredNodeConfig so the create-injected static machine.network.hostname is
// preserved, NOT the raw regenerated talosConfigs.ControlPlane()/Worker(). Staging
// the raw config would strip the hostname so a Hetzner node re-registers under a
// generated talos-xxxxx name on reboot (e.g. during a reboot-required config
// change). This drives the actual staging-path config rebuild
// (fetchAndBuildDesiredNodeConfig) with the node's running config injected via the
// fetcher seam.
func TestRolling_BuildStagedNodeConfig_PreservesNodeHostname(t *testing.T) {
	t.Parallel()

	patch := sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")
	running := runningWithHostname(
		t, talosprovisioner.RoleControlPlane, "prod-control-plane-1", patch,
	)

	require.Equal(t, "prod-control-plane-1", running.RawV1Alpha1().Hostname(),
		"precondition: running config carries the create-injected static hostname")

	// Desired configs are regenerated from the same patches — without the hostname,
	// which is a create-only post-generation transform (like registry mirrors).
	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil).
		WithNodeConfigFetcherForTest(
			func(_ context.Context, _ string) (talosconfig.Provider, error) {
				return running, nil
			},
		)

	node := talosprovisioner.NewNodeWithRoleForTest(
		"10.0.0.2", talosprovisioner.RoleControlPlane,
	)

	staged, err := prov.FetchAndBuildDesiredNodeConfigForTest(context.Background(), node, running)
	require.NoError(t, err)

	assert.Equal(t, "prod-control-plane-1", staged.RawV1Alpha1().Hostname(),
		"the staged rolling-reboot config must preserve the per-node static hostname")

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, staged)
	require.NoError(t, err)
	assert.Empty(t, diff,
		"a create-injected hostname must not read as drift in the staged config")
}

// TestRolling_BuildStagedNodeConfig_PreservesWorkerHostname covers the worker-role
// branch of the staging rebuild: the worker's running config is seeded with the
// control-plane PKI (a worker config carries no CA key — see #4963) and its
// create-injected static hostname must likewise survive staging.
func TestRolling_BuildStagedNodeConfig_PreservesWorkerHostname(t *testing.T) {
	t.Parallel()

	patch := sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")

	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	// secretsSource must be a control-plane config — a worker config carries no CA
	// key, so seeding the rebuild from the worker's own config fails ("failed to
	// parse PEM block", #4963). It need not share a PKI bundle with workerRunning:
	// this test asserts only that the create-injected hostname is grafted onto the
	// rebuilt config, which is independent of PKI alignment.
	secretsSource := runningWithHostname(
		t, talosprovisioner.RoleControlPlane, "prod-control-plane-1", patch,
	)
	workerRunning := runningWithHostname(
		t, talosprovisioner.RoleWorker, "prod-worker-1", patch,
	)

	require.Equal(t, "prod-worker-1", workerRunning.RawV1Alpha1().Hostname(),
		"precondition: worker running config carries the create-injected static hostname")

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil).
		WithNodeConfigFetcherForTest(
			func(_ context.Context, _ string) (talosconfig.Provider, error) {
				return workerRunning, nil
			},
		)

	node := talosprovisioner.NewNodeWithRoleForTest("10.0.0.3", talosprovisioner.RoleWorker)

	staged, err := prov.FetchAndBuildDesiredNodeConfigForTest(
		context.Background(),
		node,
		secretsSource,
	)
	require.NoError(t, err)

	assert.Equal(t, "prod-worker-1", staged.RawV1Alpha1().Hostname(),
		"the staged rolling-reboot config must preserve the worker's static hostname")
}
