package cluster_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/require"
)

func TestCalicoPrerequisitesAreProbedByUnchangedClusterUpdate(t *testing.T) {
	t.Parallel()
	require.Contains(t, cluster.ExportChartValuesDriftFields(), "cluster.cni.calico.prerequisites")
}

//nolint:paralleltest // overrides process-wide installer factories.
func TestUnchangedCalicoUpdateSchedulesMigrationInResolvedContext(t *testing.T) {
	regCtx := newCertManagerDriftContext()
	regCtx.ClusterCfg.Spec.Cluster.CertManager = v1alpha1.CertManagerDisabled
	regCtx.ClusterCfg.Spec.Cluster.CNI = v1alpha1.CNICalico

	var probed string

	t.Cleanup(
		cluster.SetCalicoInstallerFactoryForTests(
			func(cfg *v1alpha1.Cluster) (installer.Installer, error) {
				probed = cfg.Spec.Cluster.Connection.Context

				return &driftingInstaller{drifted: true}, nil
			},
		),
	)

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)
	require.Len(t, diff.InPlaceChanges, 1)
	require.Equal(t, "cluster.cni.calico.prerequisites", diff.InPlaceChanges[0].Field)
	require.Equal(t, cluster.ExportResolveKubeContext(regCtx), probed)
	require.Empty(t, regCtx.ClusterCfg.Spec.Cluster.Connection.Context)
}

//nolint:paralleltest // overrides process-wide installer factories.
func TestCalicoDriftDoesNotDuplicateAnExistingCNIChange(t *testing.T) {
	regCtx := newCertManagerDriftContext()
	regCtx.ClusterCfg.Spec.Cluster.CertManager = v1alpha1.CertManagerDisabled
	regCtx.ClusterCfg.Spec.Cluster.CNI = v1alpha1.CNICalico

	t.Cleanup(cluster.SetCalicoInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return &driftingInstaller{drifted: true}, nil
		},
	))

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = []clusterupdate.Change{{Field: "cluster.cni"}}
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)
	require.Len(t, diff.InPlaceChanges, 1, "the existing CNI handler already applies prerequisites")
}
