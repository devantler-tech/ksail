package cluster_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureClusterManaged_ManagedAllows verifies that a cluster present in the managed set passes
// the guard without inspecting the kubeconfig — ksail provisioned it, so the action proceeds.
func TestEnsureClusterManaged_ManagedAllows(t *testing.T) {
	t.Parallel()

	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName:    "dev",
		KubeconfigPath: "/does/not/exist",
	}
	managed := map[string]struct{}{"dev": {}}

	require.NoError(
		t,
		cluster.ExportEnsureClusterManaged(context.Background(), resolved, managed, true),
	)
}

// TestEnsureClusterManaged_DiscoveryIncompleteFailsOpen verifies that when discovery could not fully
// enumerate every provider (complete=false), the guard fails open — a transient provider failure
// (e.g. a stopped Docker daemon) must never wrongly refuse a genuine managed cluster.
func TestEnsureClusterManaged_DiscoveryIncompleteFailsOpen(t *testing.T) {
	t.Parallel()

	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName:    "external",
		KubeconfigPath: writeKubeconfigWithContext(t, t.TempDir(), "external"),
	}

	require.NoError(
		t,
		cluster.ExportEnsureClusterManaged(
			context.Background(),
			resolved,
			map[string]struct{}{},
			false,
		),
	)
}

// TestEnsureClusterManaged_UnmanagedContextRejected verifies the core behaviour (ksail#5885): a
// cluster that is NOT managed by ksail but DOES exist as a kubeconfig context is refused with a
// clear ErrUnmanagedCluster naming the cluster.
func TestEnsureClusterManaged_UnmanagedContextRejected(t *testing.T) {
	t.Parallel()

	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName:    "external-cluster",
		KubeconfigPath: writeKubeconfigWithContext(t, t.TempDir(), "external-cluster"),
	}

	err := cluster.ExportEnsureClusterManaged(
		context.Background(), resolved, map[string]struct{}{}, true,
	)

	require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
	assert.Contains(t, err.Error(), "external-cluster")
}

// TestEnsureClusterManaged_UnmanagedContextRejectedViaDetection verifies the guard maps a context to
// its ksail cluster name via detection (a Docker cluster's "kind-dev" context maps to the ksail name
// "dev"), so an unmanaged cluster is caught even when the context carries a distribution prefix.
func TestEnsureClusterManaged_UnmanagedContextRejectedViaDetection(t *testing.T) {
	t.Parallel()

	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName:    "dev",
		KubeconfigPath: writeKubeconfigWithContext(t, t.TempDir(), "kind-dev"),
	}

	err := cluster.ExportEnsureClusterManaged(
		context.Background(), resolved, map[string]struct{}{}, true,
	)

	require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
}

// TestEnsureClusterManaged_NonexistentClusterAllows verifies that a cluster absent from BOTH the
// managed set and the kubeconfig is left to the normal not-found path (guard returns nil) rather
// than being mislabelled unmanaged.
func TestEnsureClusterManaged_NonexistentClusterAllows(t *testing.T) {
	t.Parallel()

	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName:    "ghost",
		KubeconfigPath: writeKubeconfigWithContext(t, t.TempDir(), "external-cluster"),
	}

	require.NoError(
		t,
		cluster.ExportEnsureClusterManaged(
			context.Background(),
			resolved,
			map[string]struct{}{},
			true,
		),
	)
}

// TestEnsureClusterManaged_MissingKubeconfigAllows verifies that an unreadable/missing kubeconfig
// yields no rejection — with no context to prove the cluster exists, the guard cannot conclude it is
// an unmanaged cluster and so stays silent.
func TestEnsureClusterManaged_MissingKubeconfigAllows(t *testing.T) {
	t.Parallel()

	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName:    "whatever",
		KubeconfigPath: filepath.Join(t.TempDir(), "absent"),
	}

	require.NoError(
		t,
		cluster.ExportEnsureClusterManaged(
			context.Background(),
			resolved,
			map[string]struct{}{},
			true,
		),
	)
}

// TestGuardUpdateTargetManaged_UnmanagedRejected verifies that `cluster update` refuses a cluster
// ksail did not provision: when the target context exists in the kubeconfig configured in ksail.yaml
// but is NOT in ksail's managed set, guardUpdateTargetManaged rejects with ErrUnmanagedCluster before
// any configuration is applied. (ksail#5885, epic #5654.)
//
//nolint:paralleltest // mutates the package-global update guard via ExportSetUpdateUnmanagedGuard
func TestGuardUpdateTargetManaged_UnmanagedRejected(t *testing.T) {
	kubeconfigPath := writeKubeconfigWithContext(t, t.TempDir(), "kind-my-cluster")

	// Drive the REAL guard against an empty managed set: "my-cluster" is not managed, yet its context
	// exists in the configured kubeconfig, so the guard must refuse.
	restore := cluster.ExportSetUpdateUnmanagedGuard(
		func(ctx context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
			return cluster.ExportEnsureClusterManaged(ctx, resolved, map[string]struct{}{}, true)
		},
	)
	defer restore()

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Kubeconfig = kubeconfigPath

	err := cluster.ExportGuardUpdateTargetManaged(context.Background(), clusterCfg, "my-cluster")

	require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
	require.Contains(t, err.Error(), "my-cluster")
}

// TestGuardUpdateTargetManaged_ManagedAllows verifies the guard passes (returns nil) when the target
// cluster IS managed by ksail — `cluster update` proceeds normally.
//
//nolint:paralleltest // mutates the package-global update guard via ExportSetUpdateUnmanagedGuard
func TestGuardUpdateTargetManaged_ManagedAllows(t *testing.T) {
	kubeconfigPath := writeKubeconfigWithContext(t, t.TempDir(), "kind-my-cluster")

	restore := cluster.ExportSetUpdateUnmanagedGuard(
		func(ctx context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
			return cluster.ExportEnsureClusterManaged(
				ctx, resolved, map[string]struct{}{"my-cluster": {}}, true,
			)
		},
	)
	defer restore()

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Kubeconfig = kubeconfigPath

	require.NoError(
		t,
		cluster.ExportGuardUpdateTargetManaged(context.Background(), clusterCfg, "my-cluster"),
	)
}
