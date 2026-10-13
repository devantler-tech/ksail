package cluster_test

import (
	"bytes"
	"errors"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/require"
)

func TestCalicoSpecOnlyUpdateDoesNotRequireClusterRecreation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	regCtx := newCertManagerDriftContext()
	regCtx.ClusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionVCluster
	regCtx.ClusterCfg.Spec.Cluster.CertManager = v1alpha1.CertManagerDisabled
	regCtx.ClusterCfg.Spec.Cluster.CNI = v1alpha1.CNICalico
	inst := &driftingInstaller{}

	t.Cleanup(
		cluster.SetCalicoInstallerFactoryForTests(
			func(_ *v1alpha1.Cluster) (installer.Installer, error) {
				return inst, nil
			},
		),
	)

	cmd, _ := newDriftCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = []clusterupdate.Change{
		{Field: specdiff.CalicoPrerequisitesField, Category: clusterupdate.ChangeCategoryInPlace},
	}

	require.NoError(t, cluster.ExportApplySpecOnlyDiff(cmd, regCtx, "calico-only", diff, true))
	require.NotContains(
		t,
		output.String(),
		"recreation would be required",
		"Calico-only migration must preserve the cluster",
	)
	require.Zero(t, inst.installs, "dry run must not install prerequisites")
	output.Reset()
	require.NoError(t, cluster.ExportApplySpecOnlyDiff(cmd, regCtx, "calico-only", diff, false))
	require.Equal(t, 1, inst.installs)
	require.NotContains(t, output.String(), "recreat")

	saved, err := state.LoadClusterSpec("calico-only")
	require.NoError(t, err)
	require.Equal(t, v1alpha1.CNICalico, saved.CNI)
}

func TestCalicoSpecOnlyUpdatePreservesOtherChangeHandling(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		change  clusterupdate.Change
		unknown bool
	}{
		{name: "unknown baseline", unknown: true},
		{name: "node scaling", change: clusterupdate.Change{Field: "cluster.workers"}},
		{name: "CNI switch", change: clusterupdate.Change{Field: "cluster.cni"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd, _ := newDriftCmd()

			var output bytes.Buffer
			cmd.SetOut(&output)

			diff := clusterupdate.NewEmptyUpdateResult()

			diff.InPlaceChanges = []clusterupdate.Change{{Field: specdiff.CalicoPrerequisitesField}}
			if testCase.unknown {
				diff.UnknownBaseline = []clusterupdate.Change{{Field: "cluster.csi"}}
			} else {
				diff.InPlaceChanges = append(diff.InPlaceChanges, testCase.change)
			}

			require.NoError(
				t,
				cluster.ExportApplySpecOnlyDiff(
					cmd,
					newCertManagerDriftContext(),
					"other-change",
					diff,
					true,
				),
			)
			require.Contains(t, output.String(), "recreation would be required")
		})
	}
}

func TestCalicoSpecOnlyUpdateFailureDoesNotSaveDesiredState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	regCtx := newCertManagerDriftContext()
	regCtx.ClusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionVCluster
	regCtx.ClusterCfg.Spec.Cluster.CNI = v1alpha1.CNICalico
	want := errors.New("migration refused ownership") //nolint:err113 // isolated fixture sentinel.

	t.Cleanup(
		cluster.SetCalicoInstallerFactoryForTests(
			func(_ *v1alpha1.Cluster) (installer.Installer, error) {
				return nil, want
			},
		),
	)

	cmd, _ := newDriftCmd()
	cmd.SetOut(&bytes.Buffer{})

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = []clusterupdate.Change{{Field: specdiff.CalicoPrerequisitesField}}
	err := cluster.ExportApplySpecOnlyDiff(cmd, regCtx, "failed-calico", diff, false)
	require.ErrorIs(t, err, want)

	saved, loadErr := state.LoadClusterSpec("failed-calico")
	require.ErrorIs(t, loadErr, state.ErrStateNotFound)
	require.Nil(t, saved, "failed migration must not save the desired baseline")
}
