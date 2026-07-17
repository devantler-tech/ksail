package cluster_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
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

	err := cluster.ExportGuardUpdateTargetManaged(
		context.Background(), clusterCfg, "my-cluster", nil,
	)

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
		cluster.ExportGuardUpdateTargetManaged(
			context.Background(), clusterCfg, "my-cluster", nil,
		),
	)
}

// TestGuardUpdateTargetManaged_EKSPreservesAWSOwnershipContext verifies update reaches the exact
// fail-closed AWS guard rather than the legacy cross-provider discovery path. The credential aliases
// and explicit region used by the later EKS update must be identical at the ownership boundary.
func TestGuardUpdateTargetManaged_EKSPreservesAWSOwnershipContext(t *testing.T) {
	t.Setenv("KSAIL_UPDATE_REGION", "")

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	clusterCfg.Spec.Cluster.Connection.Kubeconfig = writeKubeconfigWithContext(
		t,
		t.TempDir(),
		"update-eks.eu-north-1.eksctl.io",
	)
	//nolint:gosec // these strings name test-only environment variables, not credentials
	clusterCfg.Spec.Provider.AWS = v1alpha1.OptionsAWS{
		ProfileEnvVar:         "KSAIL_UPDATE_PROFILE",
		RegionEnvVar:          "KSAIL_UPDATE_REGION",
		AccessKeyIDEnvVar:     "KSAIL_UPDATE_ACCESS",
		SecretAccessKeyEnvVar: "KSAIL_UPDATE_SECRET",
		SessionTokenEnvVar:    "KSAIL_UPDATE_SESSION",
	}
	eksConfig := &clusterprovisioner.EKSConfig{
		Name:           "update-eks",
		NameFromConfig: true,
		Region:         "eu-north-1",
	}

	var captured *lifecycle.ResolvedClusterInfo

	restore := cluster.ExportSetUpdateUnmanagedGuard(
		func(_ context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
			captured = resolved

			return nil
		},
	)
	defer restore()

	require.NoError(
		t,
		cluster.ExportGuardUpdateTargetManaged(
			context.Background(), clusterCfg, "update-eks", eksConfig,
		),
	)
	require.NotNil(t, captured)
	assert.Equal(t, v1alpha1.ProviderAWS, captured.Provider)
	assert.Equal(t, "eu-north-1", captured.AWSRegion)
	assert.Equal(t, clusterCfg.Spec.Provider.AWS, captured.AWSOpts)
}

// TestGuardUpdateTargetManaged_EKSPinsRegionBoundByOwnershipGuard verifies a region discovered by
// the exact guard is copied into the distribution config consumed by the later update/recreate
// provisioner. Validation must never bind a region and then discard it before mutation.
func TestGuardUpdateTargetManaged_EKSPinsRegionBoundByOwnershipGuard(t *testing.T) {
	t.Setenv("KSAIL_UPDATE_REGION", "")

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	clusterCfg.Spec.Cluster.Connection.Kubeconfig = filepath.Join(t.TempDir(), "missing-kubeconfig")
	clusterCfg.Spec.Provider.AWS.RegionEnvVar = "KSAIL_UPDATE_REGION"
	eksConfig := &clusterprovisioner.EKSConfig{Name: "update-eks"}

	restore := cluster.ExportSetUpdateUnmanagedGuard(
		func(_ context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
			assert.Empty(t, resolved.AWSRegion)
			resolved.AWSRegion = "eu-north-1"

			return nil
		},
	)
	defer restore()

	require.NoError(
		t,
		cluster.ExportGuardUpdateTargetManaged(
			context.Background(), clusterCfg, "update-eks", eksConfig,
		),
	)
	assert.Equal(t, "eu-north-1", eksConfig.Region)
}
