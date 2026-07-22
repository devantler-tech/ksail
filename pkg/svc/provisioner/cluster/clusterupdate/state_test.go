package clusterupdate_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain redirects $HOME to a throwaway directory so the state-persistence
// tests below never read from or write to the developer's real ~/.ksail/.
func TestMain(m *testing.M) {
	os.Exit(homeenv.Run(m))
}

// TestMergePersistedState_NoStateIsNoOp verifies that a missing state file
// leaves the baseline spec untouched and returns nil (ErrStateNotFound is
// swallowed).
func TestMergePersistedState_NoStateIsNoOp(t *testing.T) {
	t.Parallel()

	spec := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	err := clusterupdate.MergePersistedState(spec, "cluster-that-was-never-saved")
	require.NoError(t, err)
	assert.Empty(t, spec.Vanilla.MirrorsDir)
	assert.Empty(t, spec.LocalRegistry.Registry)
}

// TestMergePersistedState_NilSpecIsNoOp guards the nil-spec early return.
func TestMergePersistedState_NilSpecIsNoOp(t *testing.T) {
	t.Parallel()

	err := clusterupdate.MergePersistedState(nil, "any")
	require.NoError(t, err)
}

// TestMergePersistedState_ForwardCompatibleWithCurrentRelease persists a spec
// using the CURRENT release's state writer (state.SaveClusterSpec) — the exact
// on-disk spec.json format an already-deployed binary would have written — then
// merges it back through the hoisted helper. The non-introspectable fields
// (MirrorsDir, LocalRegistry) must survive the round-trip so a Kind/K3d update
// against a pre-hoist state file stops reporting false recreate-required diffs.
//
//nolint:paralleltest // writes/reads a cluster state file under the shared isolated $HOME
func TestMergePersistedState_ForwardCompatibleWithCurrentRelease(t *testing.T) {
	// Not parallel: SaveClusterSpec/LoadClusterSpec resolve a unique cluster name
	// under the shared isolated $HOME, so there is no cross-test collision, but we
	// keep it serial to avoid any reliance on parallel scheduling.
	const clusterName = "forward-compat-kind"

	saved := &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionVanilla,
		Provider:     v1alpha1.ProviderDocker,
		Vanilla: v1alpha1.OptionsVanilla{
			MirrorsDir: "/custom/mirrors",
		},
		LocalRegistry: v1alpha1.LocalRegistry{
			Registry: "localhost:5050",
		},
	}

	err := state.SaveClusterSpec(clusterName, saved)
	require.NoError(t, err)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	// A fresh baseline as GetCurrentConfig would build it (no MirrorsDir, no
	// LocalRegistry) — exactly the shape that produced the false diff.
	baseline := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	err = clusterupdate.MergePersistedState(baseline, clusterName)
	require.NoError(t, err)

	assert.Equal(t, "/custom/mirrors", baseline.Vanilla.MirrorsDir,
		"persisted mirrorsDir must merge back so it is not a false recreate-required diff")
	assert.Equal(t, "localhost:5050", baseline.LocalRegistry.Registry,
		"persisted localRegistry must merge back so it is not a false recreate-required diff")
}

// TestMergePersistedState_PreservesEKSLoadBalancerControllerOptIn verifies the
// non-introspectable EKS component choice comes from the last successfully
// applied spec. Both transitions matter: otherwise disable is invisible, or an
// enabled controller is offered for installation on every update.
//
//nolint:paralleltest // writes/reads cluster state files under the shared isolated $HOME
func TestMergePersistedState_PreservesEKSLoadBalancerControllerOptIn(t *testing.T) {
	for _, savedOptIn := range []bool{false, true} {
		clusterName := "eks-lb-disabled"
		if savedOptIn {
			clusterName = "eks-lb-enabled"
		}

		saved := &v1alpha1.ClusterSpec{
			Distribution: v1alpha1.DistributionEKS,
			Provider:     v1alpha1.ProviderAWS,
		}
		saved.EKS.ExperimentalAWSLoadBalancerController = savedOptIn
		require.NoError(t, state.SaveClusterSpec(clusterName, saved))
		t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

		baseline := clusterupdate.DefaultCurrentSpec(
			v1alpha1.DistributionEKS,
			v1alpha1.ProviderAWS,
		)
		baseline.EKS.ExperimentalAWSLoadBalancerController = !savedOptIn

		err := clusterupdate.MergePersistedState(baseline, clusterName)
		require.NoError(t, err)
		assert.Equal(t, savedOptIn, baseline.EKS.ExperimentalAWSLoadBalancerController)
	}
}
