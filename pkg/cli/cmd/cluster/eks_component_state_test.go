package cluster_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // writes exact-region state under the package-isolated test HOME.
func TestPersistRequiredEKSComponentStateRecordsControllerOwnership(t *testing.T) {
	const (
		clusterName = "managed-component-state"
		region      = "eu-north-1"
	)

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{},
		EKSConfig: &clusterprovisioner.EKSConfig{
			Name:   clusterName,
			Region: region,
		},
	}
	ctx.ClusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	ctx.ClusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	ctx.ClusterCfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled
	ctx.ClusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = true

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	require.NoError(t, cluster.ExportPersistRequiredEKSComponentState(ctx, clusterName))
	snapshot, err := state.LoadEKSComponentState(clusterName, region)
	require.NoError(t, err)
	assert.True(t, snapshot.AWSLoadBalancerControllerManaged)

	ctx.ClusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = false
	require.NoError(t, cluster.ExportPersistRequiredEKSComponentState(ctx, clusterName))
	snapshot, err = state.LoadEKSComponentState(clusterName, region)
	require.NoError(t, err)
	assert.False(t, snapshot.AWSLoadBalancerControllerManaged)
}

// TestPersistRequiredEKSComponentState_FailsClosed proves an applied EKS
// component mutation cannot report success when its exact-region baseline
// cannot be persisted.
//
//nolint:paralleltest // creates a deliberate path obstruction under isolated test HOME
func TestPersistRequiredEKSComponentState_FailsClosed(t *testing.T) {
	const clusterName = "unwritable-component-state"

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	clustersDir := filepath.Join(home, ".ksail", "clusters")
	require.NoError(t, os.MkdirAll(clustersDir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(clustersDir, clusterName),
		[]byte("blocked"),
		0o600,
	))

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{},
		EKSConfig: &clusterprovisioner.EKSConfig{
			Name:   clusterName,
			Region: "eu-north-1",
		},
	}
	ctx.ClusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	ctx.ClusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	ctx.ClusterCfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled
	ctx.ClusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = true

	err = cluster.ExportPersistRequiredEKSComponentState(ctx, clusterName)
	require.ErrorContains(t, err, "persist required EKS component state")
}

// TestFinishRecreateFlowPersistsEKSControllerOwnership proves the recreate path replaces a stale
// exact-region ownership marker with the controller ownership established by the new cluster.
//
//nolint:paralleltest // writes exact-region state under the package-isolated test HOME.
func TestFinishRecreateFlowPersistsEKSControllerOwnership(t *testing.T) {
	const (
		clusterName = "recreated-component-state"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
	setEKSControllerTestInstaller(t, &recordingEKSLoadBalancerInstaller{})

	require.NoError(t, state.SaveEKSComponentState(clusterName, region, &state.EKSComponentState{
		Version:                          state.EKSComponentStateVersion,
		ClusterName:                      clusterName,
		Region:                           region,
		AWSLoadBalancerControllerManaged: false,
	}))

	ctx := managedEKSComponentContext(clusterName)
	require.NoError(t, cluster.ExportFinishRecreateFlow(ctx, clusterName, nil))

	snapshot, err := state.LoadEKSComponentState(clusterName, region)
	require.NoError(t, err)
	assert.True(t, snapshot.AWSLoadBalancerControllerManaged)
}

// TestFinishRecreateFlowDoesNotClaimGitOpsManagedController proves a successful recreation does
// not infer KSail ownership when the shared installer deliberately preserved a GitOps release.
//
//nolint:paralleltest // replaces the process-global installer factory and writes state.
func TestFinishRecreateFlowDoesNotClaimGitOpsManagedController(t *testing.T) {
	const (
		clusterName = "recreated-gitops-controller"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
	setEKSControllerTestInstaller(t, &recordingEKSLoadBalancerInstaller{gitOpsManaged: true})

	ctx := managedEKSComponentContext(clusterName)
	require.NoError(t, cluster.ExportFinishRecreateFlow(ctx, clusterName, nil))

	snapshot, err := state.LoadEKSComponentState(clusterName, region)
	require.NoError(t, err)
	assert.False(t, snapshot.AWSLoadBalancerControllerManaged)
}

// TestClearDeletedEKSStateInvalidatesControllerOwnership proves recreation clears the deleted
// cluster's exact-region ownership evidence before any replacement cluster can be attempted.
//
//nolint:paralleltest // writes exact-region state under the package-isolated test HOME.
func TestClearDeletedEKSStateInvalidatesControllerOwnership(t *testing.T) {
	const (
		clusterName = "failed-recreate-component-state"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
	require.NoError(t, state.SaveEKSComponentState(clusterName, region, &state.EKSComponentState{
		Version:                          state.EKSComponentStateVersion,
		ClusterName:                      clusterName,
		Region:                           region,
		AWSLoadBalancerControllerManaged: true,
	}))

	ctx := managedEKSComponentContext(clusterName)
	require.NoError(t, cluster.ExportClearDeletedEKSState(ctx, clusterName))

	_, err := state.LoadEKSComponentState(clusterName, region)
	require.ErrorIs(t, err, state.ErrEKSComponentStateNotFound)
}

// TestApplyInPlaceChangesDoesNotClaimManualEKSController proves an unrelated successful update
// cannot infer KSail ownership solely from an already-satisfied desired controller opt-in.
//
//nolint:paralleltest // writes exact-region state under the package-isolated test HOME.
func TestApplyInPlaceChangesDoesNotClaimManualEKSController(t *testing.T) {
	const (
		clusterName = "manual-controller-unrelated-update"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	ctx := managedEKSComponentContext(clusterName)
	result := clusterupdate.NewEmptyUpdateResult()
	result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
		Field: "controlPlanes", OldValue: "1", NewValue: "3",
	})
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field: "controlPlanes", OldValue: "1", NewValue: "3",
	})

	err := cluster.ExportApplyInPlaceChanges(
		newReconcileTestCmd(),
		&fakeUpdater{result: result},
		clusterName,
		&v1alpha1.ClusterSpec{},
		ctx,
		diff,
		nil,
		false,
		false,
	)
	require.NoError(t, err)

	snapshot, err := state.LoadEKSComponentState(clusterName, region)
	require.NoError(t, err)
	assert.False(t, snapshot.AWSLoadBalancerControllerManaged)
}

// TestApplyInPlaceChangesPersistsActualEKSControllerOutcome proves exact-region ownership follows
// successful Helm activity in both directions rather than merely mirroring desired configuration.
//
//nolint:funlen,paralleltest // replaces the process-global installer factory and writes state.
func TestApplyInPlaceChangesPersistsActualEKSControllerOutcome(t *testing.T) {
	const region = "eu-north-1"

	testCases := []struct {
		name        string
		oldOptIn    bool
		newOptIn    bool
		initial     *bool
		wantManaged bool
		wantInstall int
		wantRemove  int
		installSkip bool
	}{
		{
			name: "successful install claims ownership", oldOptIn: false, newOptIn: true,
			wantManaged: true, wantInstall: 1,
		},
		{
			name: "successful uninstall releases ownership", oldOptIn: true, newOptIn: false,
			initial: new(true), wantManaged: false, wantRemove: 1,
		},
		{
			name: "GitOps-skipped install remains unowned", oldOptIn: false, newOptIn: true,
			wantManaged: false, wantInstall: 1, installSkip: true,
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "controller-outcome-" + strconv.Itoa(index)

			t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

			if testCase.initial != nil {
				require.NoError(t, state.SaveEKSComponentState(
					clusterName,
					region,
					&state.EKSComponentState{
						Version:                          state.EKSComponentStateVersion,
						ClusterName:                      clusterName,
						Region:                           region,
						AWSLoadBalancerControllerManaged: *testCase.initial,
					},
				))
			}

			fakeInstaller := &recordingEKSLoadBalancerInstaller{
				installSkipped: testCase.installSkip,
			}
			restore := cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
				func(_ *v1alpha1.Cluster) (installer.Installer, error) {
					return fakeInstaller, nil
				},
			)

			t.Cleanup(restore)

			ctx := managedEKSComponentContext(clusterName)
			ctx.ClusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = testCase.newOptIn
			diff := clusterupdate.NewEmptyUpdateResult()
			diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
				Field:    specdiff.EKSLoadBalancerControllerField,
				OldValue: strconv.FormatBool(testCase.oldOptIn),
				NewValue: strconv.FormatBool(testCase.newOptIn),
			})

			err := cluster.ExportApplyInPlaceChanges(
				newReconcileTestCmd(),
				&fakeUpdater{result: clusterupdate.NewEmptyUpdateResult()},
				clusterName,
				&v1alpha1.ClusterSpec{},
				ctx,
				diff,
				nil,
				false,
				false,
			)
			require.NoError(t, err)

			snapshot, err := state.LoadEKSComponentState(clusterName, region)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantManaged, snapshot.AWSLoadBalancerControllerManaged)
			assert.Equal(t, testCase.wantInstall, fakeInstaller.installCalls)
			assert.Equal(t, testCase.wantRemove, fakeInstaller.uninstallCalls)
		})
	}
}

func setEKSControllerTestInstaller(
	t *testing.T,
	fakeInstaller *recordingEKSLoadBalancerInstaller,
) {
	t.Helper()

	restore := cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fakeInstaller, nil
		},
	)

	t.Cleanup(restore)
}

func managedEKSComponentContext(clusterName string) *localregistry.Context {
	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{},
		EKSConfig: &clusterprovisioner.EKSConfig{
			Name:   clusterName,
			Region: "eu-north-1",
		},
	}
	ctx.ClusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	ctx.ClusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	ctx.ClusterCfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled
	ctx.ClusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = true

	return ctx
}
