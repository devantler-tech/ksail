package cluster_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	awslbcontrollerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/awslbcontroller"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingEKSLoadBalancerInstaller struct {
	installCalls       int
	uninstallCalls     int
	gitOpsManagedCalls int
	installSkipped     bool
	gitOpsManaged      bool
	releaseIdentity    string
	releaseIdentityErr error
	uninstallErr       error
}

func (r *recordingEKSLoadBalancerInstaller) Install(_ context.Context) error {
	r.installCalls++

	return nil
}

func (r *recordingEKSLoadBalancerInstaller) InstallWithResult(ctx context.Context) (bool, error) {
	err := r.Install(ctx)

	return !r.installSkipped, err
}

func (r *recordingEKSLoadBalancerInstaller) IsGitOpsManaged(context.Context) (bool, error) {
	r.gitOpsManagedCalls++

	return r.gitOpsManaged, nil
}

func (r *recordingEKSLoadBalancerInstaller) ReleaseIdentity(context.Context) (string, error) {
	if r.releaseIdentityErr != nil {
		return "", r.releaseIdentityErr
	}

	if r.releaseIdentity == "" {
		return "release-uid", nil
	}

	return r.releaseIdentity, nil
}

func (r *recordingEKSLoadBalancerInstaller) Uninstall(_ context.Context) error {
	r.uninstallCalls++

	return r.uninstallErr
}

func (r *recordingEKSLoadBalancerInstaller) Images(_ context.Context) ([]string, error) {
	return nil, nil
}

var _ installer.Installer = (*recordingEKSLoadBalancerInstaller)(nil)

func persistManagedEKSControllerState(t *testing.T, region string) {
	t.Helper()
	saveTestEKSOwnership(t, "test-cluster", region)

	require.NoError(t, state.SaveEKSComponentState(
		"test-cluster",
		region,
		&state.EKSComponentState{
			Version:                                  state.EKSComponentStateVersion,
			ClusterName:                              "test-cluster",
			Region:                                   region,
			AccountID:                                testEKSComponentAccountID,
			AWSLoadBalancerControllerManaged:         true,
			AWSLoadBalancerControllerReleaseIdentity: "release-uid",
		},
	))
	t.Cleanup(func() { _ = state.DeleteClusterState("test-cluster") })
}

type eksOptInTransitionCase struct {
	name               string
	oldValue           bool
	newValue           bool
	wantInstallCalls   int
	wantUninstallCalls int
}

//nolint:paralleltest // subtests replace the process-global installer factory.
func TestReconcileLoadBalancer_EKSOptInTransitionsReachHelm(t *testing.T) {
	const region = "eu-north-1"

	tests := []eksOptInTransitionCase{
		{
			name: "enable installs", oldValue: false, newValue: true,
			wantInstallCalls: 1,
		},
		{
			name: "disable uninstalls", oldValue: true, newValue: false,
			wantUninstallCalls: 1,
		},
	}

	for _, testCase := range tests {
		//nolint:paralleltest // each case replaces the process-global installer factory.
		t.Run(testCase.name, func(t *testing.T) {
			runEKSOptInTransitionCase(t, testCase, region)
		})
	}
}

func runEKSOptInTransitionCase(
	t *testing.T,
	testCase eksOptInTransitionCase,
	region string,
) {
	t.Helper()

	fakeInstaller := &recordingEKSLoadBalancerInstaller{}
	factoryCalls := 0
	restore := cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			factoryCalls++

			return fakeInstaller, nil
		},
	)
	t.Cleanup(restore)

	if testCase.wantUninstallCalls > 0 {
		persistManagedEKSControllerState(t, region)
	}

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	clusterCfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled
	clusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = testCase.newValue

	detected := clusterupdate.NewEmptyUpdateResult()
	detected.InPlaceChanges = append(detected.InPlaceChanges, clusterupdate.Change{
		Field:    specdiff.EKSLoadBalancerControllerField,
		OldValue: strconv.FormatBool(testCase.oldValue),
		NewValue: strconv.FormatBool(testCase.newValue),
	})
	applied := clusterupdate.NewEmptyUpdateResult()

	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), clusterCfg, detected, applied, region,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, factoryCalls)
	assert.Equal(t, testCase.wantInstallCalls, fakeInstaller.installCalls)
	assert.Equal(t, testCase.wantUninstallCalls, fakeInstaller.uninstallCalls)
	assert.Len(t, applied.AppliedChanges, 1)
}

//nolint:paralleltest // replaces the process-global installer factory.
func TestReconcileLoadBalancer_EKSManualReleaseIsPreserved(t *testing.T) {
	fakeInstaller := &recordingEKSLoadBalancerInstaller{}
	factoryCalls := 0
	restore := cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
		func(
			_ *v1alpha1.Cluster,
		) (installer.Installer, error) {
			factoryCalls++

			return fakeInstaller, nil
		},
	)
	t.Cleanup(restore)

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	clusterCfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled

	detected := clusterupdate.NewEmptyUpdateResult()
	detected.InPlaceChanges = append(detected.InPlaceChanges, clusterupdate.Change{
		Field:    specdiff.EKSLoadBalancerControllerField,
		OldValue: "true",
		NewValue: "false",
	})
	applied := clusterupdate.NewEmptyUpdateResult()

	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), clusterCfg, detected, applied,
	)

	require.Error(t, err)
	assert.Zero(t, factoryCalls)
	assert.Zero(t, fakeInstaller.uninstallCalls)
	assert.Empty(t, applied.AppliedChanges)
	require.Len(t, applied.FailedChanges, 1)
	assert.Contains(t, applied.FailedChanges[0].Reason, "ownership")
}

// TestReconcileLoadBalancer_EKSReplacementInvalidatesOwnership proves a stale
// marker from a deleted KSail release cannot authorize deleting a later,
// same-name Helm release installed by somebody else.
//
//nolint:paralleltest // replaces the process-global installer factory and writes state.
func TestReconcileLoadBalancer_EKSReplacementInvalidatesOwnership(t *testing.T) {
	const region = "eu-north-1"

	fakeInstaller := &recordingEKSLoadBalancerInstaller{releaseIdentity: "replacement-uid"}
	restore := cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fakeInstaller, nil
		},
	)
	t.Cleanup(restore)
	persistManagedEKSControllerState(t, region)

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	clusterCfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled

	detected := clusterupdate.NewEmptyUpdateResult()
	detected.InPlaceChanges = append(detected.InPlaceChanges, clusterupdate.Change{
		Field: specdiff.EKSLoadBalancerControllerField, OldValue: "true", NewValue: "false",
	})
	applied := clusterupdate.NewEmptyUpdateResult()

	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), clusterCfg, detected, applied, region,
	)

	require.Error(t, err)
	assert.Zero(t, fakeInstaller.uninstallCalls)
	assert.Empty(t, applied.AppliedChanges)
	require.Len(t, applied.FailedChanges, 1)
	assert.Contains(t, applied.FailedChanges[0].Reason, "ownership")
}

// TestReconcileLoadBalancer_EKSGitOpsTakeoverRemainsUnresolved proves a
// release that becomes GitOps-managed is preserved without advancing the
// disabled baseline or clearing KSail's prior ownership evidence.
//
//nolint:paralleltest // replaces the process-global installer factory and writes state.
func TestReconcileLoadBalancer_EKSGitOpsTakeoverRemainsUnresolved(t *testing.T) {
	const region = "eu-north-1"

	fakeInstaller := &recordingEKSLoadBalancerInstaller{
		uninstallErr: awslbcontrollerinstaller.ErrGitOpsManagedUninstallSkipped,
	}
	restore := cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fakeInstaller, nil
		},
	)
	t.Cleanup(restore)
	persistManagedEKSControllerState(t, region)

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	clusterCfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled

	detected := clusterupdate.NewEmptyUpdateResult()
	detected.InPlaceChanges = append(detected.InPlaceChanges, clusterupdate.Change{
		Field: specdiff.EKSLoadBalancerControllerField, OldValue: "true", NewValue: "false",
	})
	applied := clusterupdate.NewEmptyUpdateResult()

	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), clusterCfg, detected, applied, region,
	)

	require.ErrorIs(t, err, awslbcontrollerinstaller.ErrGitOpsManagedUninstallSkipped)
	assert.Equal(t, 1, fakeInstaller.uninstallCalls)
	assert.Empty(t, applied.AppliedChanges)
	require.Len(t, applied.FailedChanges, 1)

	snapshot, loadErr := state.LoadEKSComponentState(
		"test-cluster",
		region,
		testEKSComponentAccountID,
	)
	require.NoError(t, loadErr)
	assert.True(t, snapshot.AWSLoadBalancerControllerManaged)
	assert.Equal(t, "release-uid", snapshot.AWSLoadBalancerControllerReleaseIdentity)
}

// TestReconcileLoadBalancer_EKSRemovedReleaseClearsOwnership proves an absent
// release is a safe no-op rather than a permanent stale-ownership failure.
//
//nolint:paralleltest // replaces the process-global installer factory and writes state.
func TestReconcileLoadBalancer_EKSRemovedReleaseClearsOwnership(t *testing.T) {
	const region = "eu-north-1"

	fakeInstaller := &recordingEKSLoadBalancerInstaller{
		releaseIdentityErr: helm.ErrNoReleaseStorage,
	}
	restore := cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fakeInstaller, nil
		},
	)
	t.Cleanup(restore)
	persistManagedEKSControllerState(t, region)

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	clusterCfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled
	detected := clusterupdate.NewEmptyUpdateResult()
	detected.InPlaceChanges = append(detected.InPlaceChanges, clusterupdate.Change{
		Field: specdiff.EKSLoadBalancerControllerField, OldValue: "true", NewValue: "false",
	})
	applied := clusterupdate.NewEmptyUpdateResult()

	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), clusterCfg, detected, applied, region,
	)

	require.NoError(t, err)
	assert.Zero(t, fakeInstaller.uninstallCalls)
	assert.Len(t, applied.AppliedChanges, 1)
}

//nolint:funlen,paralleltest // table coverage replaces the process-global installer factory.
func TestReconcileComponents_EKSLoadBalancerChangesCoalesce(t *testing.T) {
	const region = "eu-north-1"

	tests := []struct {
		name                string
		desiredLoadBalancer v1alpha1.LoadBalancer
		desiredOptIn        bool
		genericOld          string
		genericNew          string
		optInOld            string
		optInNew            string
		wantInstallCalls    int
		wantUninstallCalls  int
	}{
		{
			name:                "enable installs once",
			desiredLoadBalancer: v1alpha1.LoadBalancerEnabled,
			desiredOptIn:        true,
			genericOld:          string(v1alpha1.LoadBalancerDisabled),
			genericNew:          string(v1alpha1.LoadBalancerEnabled),
			optInOld:            "false",
			optInNew:            "true",
			wantInstallCalls:    1,
		},
		{
			name:                "disable uninstalls once",
			desiredLoadBalancer: v1alpha1.LoadBalancerDisabled,
			desiredOptIn:        false,
			genericOld:          string(v1alpha1.LoadBalancerEnabled),
			genericNew:          string(v1alpha1.LoadBalancerDisabled),
			optInOld:            "true",
			optInNew:            "false",
			wantUninstallCalls:  1,
		},
	}

	for _, testCase := range tests {
		//nolint:paralleltest // each case replaces the process-global installer factory.
		t.Run(testCase.name, func(t *testing.T) {
			fakeInstaller := &recordingEKSLoadBalancerInstaller{}
			restore := cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
				func(
					_ *v1alpha1.Cluster,
				) (installer.Installer, error) {
					return fakeInstaller, nil
				},
			)
			t.Cleanup(restore)

			if testCase.wantUninstallCalls > 0 {
				persistManagedEKSControllerState(t, region)
			}

			clusterCfg := &v1alpha1.Cluster{}
			clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
			clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
			clusterCfg.Spec.Cluster.LoadBalancer = testCase.desiredLoadBalancer
			clusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = testCase.desiredOptIn

			detected := clusterupdate.NewEmptyUpdateResult()
			detected.InPlaceChanges = append(
				detected.InPlaceChanges,
				clusterupdate.Change{
					Field:    "cluster.loadBalancer",
					OldValue: testCase.genericOld,
					NewValue: testCase.genericNew,
				},
				clusterupdate.Change{
					Field:    specdiff.EKSLoadBalancerControllerField,
					OldValue: testCase.optInOld,
					NewValue: testCase.optInNew,
				},
			)
			applied := clusterupdate.NewEmptyUpdateResult()
			err := cluster.ExportReconcileComponents(
				newReconcileTestCmd(), clusterCfg, detected, applied, region,
			)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantInstallCalls, fakeInstaller.installCalls)
			assert.Equal(t, testCase.wantUninstallCalls, fakeInstaller.uninstallCalls)
			assert.Len(t, applied.AppliedChanges, 2)
		})
	}
}
