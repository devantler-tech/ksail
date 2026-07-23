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

func changeFields(changes []clusterupdate.Change) []string {
	fields := make([]string, 0, len(changes))
	for _, change := range changes {
		fields = append(fields, change.Field)
	}

	return fields
}

//nolint:paralleltest // writes exact-region state under the package-isolated test HOME.
func TestPersistRequiredEKSComponentStateRecordsControllerOwnership(t *testing.T) {
	const (
		clusterName = "managed-component-state"
		region      = "eu-north-1"
	)

	ctx := &localregistry.Context{
		ClusterCfg:   &v1alpha1.Cluster{},
		EKSAccountID: testEKSComponentAccountID,
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
	snapshot, err := state.LoadEKSComponentState(clusterName, region, testEKSComponentAccountID)
	require.NoError(t, err)
	assert.True(t, snapshot.AWSLoadBalancerControllerManaged)

	ctx.ClusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = false
	require.NoError(t, cluster.ExportPersistRequiredEKSComponentState(ctx, clusterName))
	snapshot, err = state.LoadEKSComponentState(clusterName, region, testEKSComponentAccountID)
	require.NoError(t, err)
	assert.False(t, snapshot.AWSLoadBalancerControllerManaged)
}

//nolint:paralleltest // writes exact-region state under the package-isolated test HOME.
func TestOverlayOwnedEKSControllerCleanupBaselineExposesFailedReleaseForRemoval(t *testing.T) {
	const (
		clusterName = "failed-owned-controller"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
	saveTestEKSOwnership(t, clusterName, region)
	require.NoError(t, state.SaveEKSComponentState(clusterName, region, &state.EKSComponentState{
		Version:                                  state.EKSComponentStateVersion,
		ClusterName:                              clusterName,
		Region:                                   region,
		AccountID:                                testEKSComponentAccountID,
		AWSLoadBalancerControllerManaged:         true,
		AWSLoadBalancerControllerReleaseIdentity: "failed-release-uid",
	}))

	current := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionEKS,
		v1alpha1.ProviderAWS,
	)
	desired := *current
	desired.LoadBalancer = v1alpha1.LoadBalancerDisabled
	desired.EKS.ExperimentalAWSLoadBalancerController = false

	require.NoError(t, cluster.ExportOverlayOwnedEKSControllerCleanupBaseline(
		current,
		&desired,
		clusterName,
		region,
	))
	assert.Equal(t, v1alpha1.LoadBalancerEnabled, current.LoadBalancer)
	assert.True(
		t,
		current.EKS.ExperimentalAWSLoadBalancerController,
		"positive ownership must expose inactive Helm history as a removal diff",
	)

	result := specdiff.NewEngine(v1alpha1.DistributionEKS, v1alpha1.ProviderAWS).
		ComputeDiff(current, &desired, nil, nil)
	assert.Contains(t, changeFields(result.InPlaceChanges), specdiff.EKSLoadBalancerControllerField)
}

//nolint:paralleltest // writes exact-region state under the package-isolated test HOME.
func TestOverlayOwnedEKSControllerCleanupBaselineDoesNotMaskMissingDesiredRelease(t *testing.T) {
	const (
		clusterName = "missing-desired-controller"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
	saveTestEKSOwnership(t, clusterName, region)
	require.NoError(t, state.SaveEKSComponentState(clusterName, region, &state.EKSComponentState{
		Version:                                  state.EKSComponentStateVersion,
		ClusterName:                              clusterName,
		Region:                                   region,
		AccountID:                                testEKSComponentAccountID,
		AWSLoadBalancerControllerManaged:         true,
		AWSLoadBalancerControllerReleaseIdentity: "missing-release-uid",
	}))

	current := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionEKS,
		v1alpha1.ProviderAWS,
	)
	desired := *current
	desired.LoadBalancer = v1alpha1.LoadBalancerEnabled
	desired.EKS.ExperimentalAWSLoadBalancerController = true

	require.NoError(t, cluster.ExportOverlayOwnedEKSControllerCleanupBaseline(
		current,
		&desired,
		clusterName,
		region,
	))
	assert.False(
		t,
		current.EKS.ExperimentalAWSLoadBalancerController,
		"a desired but missing release must remain inactive so update reinstalls it",
	)
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
		ClusterCfg:   &v1alpha1.Cluster{},
		EKSAccountID: testEKSComponentAccountID,
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
		AccountID:                        testEKSComponentAccountID,
		AWSLoadBalancerControllerManaged: false,
	}))

	ctx := managedEKSComponentContext(clusterName)
	require.NoError(t, cluster.ExportFinishRecreateFlow(ctx, clusterName, nil, true))

	snapshot, err := state.LoadEKSComponentState(clusterName, region, testEKSComponentAccountID)
	require.NoError(t, err)
	assert.True(t, snapshot.AWSLoadBalancerControllerManaged)
	assert.Equal(t, "release-uid", snapshot.AWSLoadBalancerControllerReleaseIdentity)
}

// TestFinishRecreateFlowPersistsControllerOwnershipAfterPartialFailure proves
// a later create-phase failure cannot orphan an already-installed controller.
//
//nolint:paralleltest // replaces the process-global installer factory and writes state.
func TestFinishRecreateFlowPersistsControllerOwnershipAfterPartialFailure(t *testing.T) {
	const (
		clusterName = "partial-recreate-component-state"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
	setEKSControllerTestInstaller(t, &recordingEKSLoadBalancerInstaller{})

	creationErr := assert.AnError
	err := cluster.ExportFinishRecreateFlow(
		managedEKSComponentContext(clusterName),
		clusterName,
		creationErr,
		true,
	)

	require.ErrorIs(t, err, creationErr)

	snapshot, loadErr := state.LoadEKSComponentState(
		clusterName,
		region,
		testEKSComponentAccountID,
	)
	require.NoError(t, loadErr)
	assert.True(t, snapshot.AWSLoadBalancerControllerManaged)
	assert.Equal(t, "release-uid", snapshot.AWSLoadBalancerControllerReleaseIdentity)
}

// TestFinishRecreateFlowSkipsOwnershipLookupAfterTotalCreationFailure proves
// an error before post-CNI reconciliation does not add a live Helm round trip
// to the original creation failure.
//
//nolint:paralleltest // replaces the process-global installer factory and writes state.
func TestFinishRecreateFlowSkipsOwnershipLookupAfterTotalCreationFailure(t *testing.T) {
	const (
		clusterName = "failed-recreate-component-state"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	installer := &recordingEKSLoadBalancerInstaller{}
	setEKSControllerTestInstaller(t, installer)

	creationErr := assert.AnError
	err := cluster.ExportFinishRecreateFlow(
		managedEKSComponentContext(clusterName),
		clusterName,
		creationErr,
		false,
	)

	require.ErrorIs(t, err, creationErr)
	assert.Zero(t, installer.gitOpsManagedCalls)

	_, loadErr := state.LoadEKSComponentState(clusterName, region, testEKSComponentAccountID)
	require.ErrorIs(t, loadErr, state.ErrEKSComponentStateNotFound)
}

// TestFinishRecreateFlowPersistsReplacementClusterSpec proves recreation
// restores the declarative baseline cleared immediately after deletion.
//
//nolint:paralleltest // replaces the process-global installer factory and writes state.
func TestFinishRecreateFlowPersistsReplacementClusterSpec(t *testing.T) {
	const clusterName = "recreated-spec-state"

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
	setEKSControllerTestInstaller(t, &recordingEKSLoadBalancerInstaller{})

	oldSpec := &v1alpha1.ClusterSpec{}
	oldSpec.Distribution = v1alpha1.DistributionEKS
	oldSpec.Provider = v1alpha1.ProviderAWS
	require.NoError(t, state.SaveClusterSpec(clusterName, oldSpec))

	ctx := managedEKSComponentContext(clusterName)
	ctx.ClusterCfg.Spec.Cluster.EKS.AWSLoadBalancerControllerServiceAccount = "replacement-sa"
	require.NoError(t, cluster.ExportFinishRecreateFlow(ctx, clusterName, nil, true))

	snapshot, err := state.LoadClusterSpec(clusterName)
	require.NoError(t, err)
	assert.True(t, snapshot.EKS.ExperimentalAWSLoadBalancerController)
	assert.Equal(t, "replacement-sa", snapshot.EKS.AWSLoadBalancerControllerServiceAccount)
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
	require.NoError(t, cluster.ExportFinishRecreateFlow(ctx, clusterName, nil, true))

	snapshot, err := state.LoadEKSComponentState(clusterName, region, testEKSComponentAccountID)
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
		Version:                                  state.EKSComponentStateVersion,
		ClusterName:                              clusterName,
		Region:                                   region,
		AccountID:                                testEKSComponentAccountID,
		AWSLoadBalancerControllerManaged:         true,
		AWSLoadBalancerControllerReleaseIdentity: "release-uid",
	}))

	ctx := managedEKSComponentContext(clusterName)
	require.NoError(t, cluster.ExportClearDeletedEKSState(ctx, clusterName))

	_, err := state.LoadEKSComponentState(clusterName, region, testEKSComponentAccountID)
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
	saveTestEKSOwnership(t, clusterName, region)

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

	snapshot, err := state.LoadEKSComponentState(clusterName, region, testEKSComponentAccountID)
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
		wantErr     bool
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
			name:        "GitOps-skipped install remains unowned",
			oldOptIn:    false,
			newOptIn:    true,
			initial:     new(false),
			wantManaged: false,
			wantInstall: 1,
			installSkip: true,
			wantErr:     true,
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "controller-outcome-" + strconv.Itoa(index)

			t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
			saveTestEKSOwnership(t, clusterName, region)

			if testCase.initial != nil {
				require.NoError(t, state.SaveEKSComponentState(
					clusterName,
					region,
					func() *state.EKSComponentState {
						snapshot := &state.EKSComponentState{
							Version:                          state.EKSComponentStateVersion,
							ClusterName:                      clusterName,
							Region:                           region,
							AccountID:                        testEKSComponentAccountID,
							AWSLoadBalancerControllerManaged: *testCase.initial,
						}
						if *testCase.initial {
							snapshot.AWSLoadBalancerControllerReleaseIdentity = "release-uid"
						}

						return snapshot
					}(),
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
			if testCase.wantErr {
				require.ErrorIs(t, err, cluster.ErrUpdateChangesFailed)
			} else {
				require.NoError(t, err)
			}

			snapshot, err := state.LoadEKSComponentState(
				clusterName,
				region,
				testEKSComponentAccountID,
			)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantManaged, snapshot.AWSLoadBalancerControllerManaged)
			assert.Equal(t, testCase.wantInstall, fakeInstaller.installCalls)
			assert.Equal(t, testCase.wantRemove, fakeInstaller.uninstallCalls)
		})
	}
}

// TestApplyInPlaceChangesRejectsSkippedEKSControllerMutation proves a GitOps
// skip cannot advance the requested service-account baseline as though Helm
// converged it.
//
//nolint:paralleltest // replaces the process-global installer factory and writes state.
func TestApplyInPlaceChangesRejectsSkippedEKSControllerMutation(t *testing.T) {
	const (
		clusterName = "skipped-controller-update"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
	saveTestEKSOwnership(t, clusterName, region)
	require.NoError(t, state.SaveEKSComponentState(clusterName, region, &state.EKSComponentState{
		Version: state.EKSComponentStateVersion, ClusterName: clusterName, Region: region,
		AccountID:                               testEKSComponentAccountID,
		AWSLoadBalancerControllerServiceAccount: "old-service-account",
	}))
	setEKSControllerTestInstaller(t, &recordingEKSLoadBalancerInstaller{installSkipped: true})

	ctx := managedEKSComponentContext(clusterName)
	ctx.ClusterCfg.Spec.Cluster.EKS.AWSLoadBalancerControllerServiceAccount = "new-service-account"
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    specdiff.EKSLoadBalancerControllerField,
		OldValue: "enabled|serviceAccount=old-service-account",
		NewValue: "enabled|serviceAccount=new-service-account",
	})

	err := cluster.ExportApplyInPlaceChanges(
		newReconcileTestCmd(), &fakeUpdater{result: clusterupdate.NewEmptyUpdateResult()},
		clusterName, &v1alpha1.ClusterSpec{}, ctx, diff, nil, false, false,
	)

	require.ErrorIs(t, err, cluster.ErrUpdateChangesFailed)

	snapshot, loadErr := state.LoadEKSComponentState(
		clusterName,
		region,
		testEKSComponentAccountID,
	)
	require.NoError(t, loadErr)
	assert.False(t, snapshot.AWSLoadBalancerControllerManaged)
	assert.Equal(t, "old-service-account", snapshot.AWSLoadBalancerControllerServiceAccount)
}

// TestApplyInPlaceChangesPersistsControllerMutationOnPartialFailure proves an
// unrelated failed change cannot discard ownership evidence for a Helm
// mutation that already succeeded in the same reconciliation pass.
//
//nolint:paralleltest // replaces the process-global installer factory and writes state.
func TestApplyInPlaceChangesPersistsControllerMutationOnPartialFailure(t *testing.T) {
	const (
		clusterName = "partial-controller-update"
		region      = "eu-north-1"
	)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })
	setEKSControllerTestInstaller(t, &recordingEKSLoadBalancerInstaller{
		releaseIdentity: "partial-success-uid",
	})

	restoreCSI := cluster.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restoreCSI)

	ctx := managedEKSComponentContext(clusterName)
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(
		diff.InPlaceChanges,
		clusterupdate.Change{
			Field: specdiff.EKSLoadBalancerControllerField, OldValue: "false", NewValue: "true",
		},
		clusterupdate.Change{
			Field: "cluster.csi", OldValue: string(v1alpha1.CSIDisabled), NewValue: "ebs",
		},
	)

	err := cluster.ExportApplyInPlaceChanges(
		newReconcileTestCmd(),
		&fakeUpdater{result: clusterupdate.NewEmptyUpdateResult()},
		clusterName,
		&v1alpha1.ClusterSpec{}, ctx, diff, nil, false, false,
	)

	require.ErrorIs(t, err, cluster.ErrUpdateChangesFailed)

	snapshot, loadErr := state.LoadEKSComponentState(
		clusterName,
		region,
		testEKSComponentAccountID,
	)
	require.NoError(t, loadErr)
	assert.True(t, snapshot.AWSLoadBalancerControllerManaged)
	assert.Equal(t, "partial-success-uid", snapshot.AWSLoadBalancerControllerReleaseIdentity)
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
		ClusterCfg:   &v1alpha1.Cluster{},
		EKSAccountID: testEKSComponentAccountID,
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
