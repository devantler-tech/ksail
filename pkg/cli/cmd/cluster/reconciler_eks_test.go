package cluster_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingEKSLoadBalancerInstaller struct {
	installCalls   int
	uninstallCalls int
}

func (r *recordingEKSLoadBalancerInstaller) Install(_ context.Context) error {
	r.installCalls++

	return nil
}

func (r *recordingEKSLoadBalancerInstaller) Uninstall(_ context.Context) error {
	r.uninstallCalls++

	return nil
}

func (r *recordingEKSLoadBalancerInstaller) Images(_ context.Context) ([]string, error) {
	return nil, nil
}

var _ installer.Installer = (*recordingEKSLoadBalancerInstaller)(nil)

//nolint:paralleltest // subtests replace the process-global installer factory.
func TestReconcileLoadBalancer_EKSOptInTransitionsReachHelm(t *testing.T) {
	tests := []struct {
		name               string
		oldValue           bool
		newValue           bool
		wantInstallCalls   int
		wantUninstallCalls int
	}{
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
			clusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = testCase.newValue

			detected := clusterupdate.NewEmptyUpdateResult()
			detected.InPlaceChanges = append(detected.InPlaceChanges, clusterupdate.Change{
				Field:    specdiff.EKSLoadBalancerControllerField,
				OldValue: strconv.FormatBool(testCase.oldValue),
				NewValue: strconv.FormatBool(testCase.newValue),
			})
			applied := clusterupdate.NewEmptyUpdateResult()

			err := cluster.ExportReconcileComponents(
				newReconcileTestCmd(), clusterCfg, detected, applied,
			)

			require.NoError(t, err)
			assert.Equal(t, 1, factoryCalls)
			assert.Equal(t, testCase.wantInstallCalls, fakeInstaller.installCalls)
			assert.Equal(t, testCase.wantUninstallCalls, fakeInstaller.uninstallCalls)
			assert.Len(t, applied.AppliedChanges, 1)
		})
	}
}

//nolint:funlen,paralleltest // table coverage replaces the process-global installer factory.
func TestReconcileComponents_EKSLoadBalancerChangesCoalesce(t *testing.T) {
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
				newReconcileTestCmd(), clusterCfg, detected, applied,
			)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantInstallCalls, fakeInstaller.installCalls)
			assert.Equal(t, testCase.wantUninstallCalls, fakeInstaller.uninstallCalls)
			assert.Len(t, applied.AppliedChanges, 2)
		})
	}
}
