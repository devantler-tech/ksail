package cluster_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newHetznerCSIDriftContext is a Talos cluster on Hetzner with the CSI setting
// left at Default, which on that combination has KSail install the Hetzner CSI
// driver.
func newHetznerCSIDriftContext() *localregistry.Context {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
	cfg.Spec.Cluster.CSI = v1alpha1.CSIDefault

	return &localregistry.Context{ClusterCfg: cfg}
}

// TestCheckChartValuesDrift_ReportsHetznerCSIDrift is ksail#7747: an installed
// Hetzner CSI release whose values differ from the rendered ones must surface
// as an in-place change, probed through the resolved context.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_ReportsHetznerCSIDrift(t *testing.T) {
	probed := assertReportsValuesDrift(t, newHetznerCSIDriftContext(),
		cluster.SetCSIInstallerFactoryForTests, specdiff.CSIValuesField)

	assert.Equal(t, v1alpha1.ProviderHetzner, probed.Spec.Cluster.Provider)
}

// TestCheckChartValuesDrift_StaysSilentForMatchingHetznerCSI is the control.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_StaysSilentForMatchingHetznerCSI(t *testing.T) {
	probes := 0

	t.Cleanup(cluster.SetCSIInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			probes++

			return &driftingInstaller{}, nil
		},
	))

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newHetznerCSIDriftContext(), diff)

	assert.Equal(t, 1, probes, "the control must have compared the release")
	assert.Zero(t, diff.TotalChanges())
}

// TestCheckChartValuesDrift_SkipsCSIKSailDoesNotInstallThroughHelm never probes
// a storage driver KSail does not install through Helm: a disabled driver has
// no release, and every cluster that is not Talos on Hetzner gets the
// local-path provisioner (or its distribution's own), which is not a release.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_SkipsCSIKSailDoesNotInstallThroughHelm(t *testing.T) {
	for name, configure := range map[string]func(*v1alpha1.Cluster){
		"disabled": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.CSI = v1alpha1.CSIDisabled
		},
		"talos on docker": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
			cfg.Spec.Cluster.CSI = v1alpha1.CSIEnabled
		},
		"vanilla local-path": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
			cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
			cfg.Spec.Cluster.CSI = v1alpha1.CSIEnabled
		},
	} {
		t.Run(name, func(t *testing.T) {
			probes := 0

			t.Cleanup(cluster.SetCSIInstallerFactoryForTests(
				func(*v1alpha1.Cluster) (installer.Installer, error) {
					probes++

					return &driftingInstaller{drifted: true}, nil
				},
			))

			regCtx := newHetznerCSIDriftContext()
			configure(regCtx.ClusterCfg)

			cmd, stderr := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			assert.Zero(t, probes, "the CSI driver must not be probed")
			assert.Zero(t, diff.TotalChanges())
			assert.Empty(t, stderr.String(), "a release nobody installed must not warn")
		})
	}
}

// TestCheckChartValuesDrift_WarnsWhenHetznerCSICannotBeCompared never turns an
// unreadable release into a clean verdict silently: the update continues, but
// the operator is told the comparison did not happen.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_WarnsWhenHetznerCSICannotBeCompared(t *testing.T) {
	t.Cleanup(cluster.SetCSIInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			return &driftingInstaller{err: errValuesProbeFailed}, nil
		},
	))

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newHetznerCSIDriftContext(), diff)

	assert.Zero(t, diff.TotalChanges())
	assert.Contains(t, stderr.String(), "Cannot compare hetzner-csi values")
}

// TestReconcileCSIValuesSharesOneUpgrade coalesces a cluster.csi change and
// Hetzner CSI values drift in one pass into one Helm upgrade, and replays its
// outcome for both fields.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileCSIValuesSharesOneUpgrade(t *testing.T) {
	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{
			Field:    "cluster.csi",
			OldValue: string(v1alpha1.CSIDisabled),
			NewValue: string(v1alpha1.CSIEnabled),
		},
		{Field: specdiff.CSIValuesField},
	}}
	cfg := newHetznerCSIDriftContext().ClusterCfg
	cfg.Spec.Cluster.CSI = v1alpha1.CSIEnabled

	t.Run("success", func(t *testing.T) {
		inst := &driftingInstaller{}

		t.Cleanup(cluster.SetCSIInstallerFactoryForTests(
			func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
		))

		result := clusterupdate.NewEmptyUpdateResult()
		err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)
		require.NoError(t, err)
		assert.Equal(t, 1, inst.installs, "both fields share one CSI upgrade")
		assert.Len(t, result.AppliedChanges, 2)
	})

	t.Run("failure", func(t *testing.T) {
		builds := 0

		t.Cleanup(cluster.SetCSIInstallerFactoryForTests(
			func(*v1alpha1.Cluster) (installer.Installer, error) {
				builds++

				return nil, errValuesProbeFailed
			},
		))

		result := clusterupdate.NewEmptyUpdateResult()
		err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)
		require.ErrorIs(t, err, errValuesProbeFailed)
		assert.Equal(t, 1, builds, "the failed upgrade must not be retried per field")
		assert.Len(t, result.FailedChanges, 2, "the first failure must survive deduplication")
	})
}

// TestReconcileCSIValues_UpgradesOnValuesOnlyDrift applies drift on its own: an
// unchanged ksail.yaml still upgrades the drifted release once.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileCSIValues_UpgradesOnValuesOnlyDrift(t *testing.T) {
	inst := &driftingInstaller{drifted: true}

	t.Cleanup(cluster.SetCSIInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	regCtx := newHetznerCSIDriftContext()
	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

	require.Len(t, diff.InPlaceChanges, 1, "the values-only change must be reported")

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(cmd, regCtx.ClusterCfg, diff, result)
	require.NoError(t, err)
	assert.Equal(t, 1, inst.installs)
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, specdiff.CSIValuesField, result.AppliedChanges[0].Field)
}

// TestReconcileCSIValues_SkipsWhenKSailDoesNotInstallIt re-checks the
// configuration at apply time, so a stale drift change never installs a
// storage driver the configuration does not want.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileCSIValues_SkipsWhenKSailDoesNotInstallIt(t *testing.T) {
	inst := &driftingInstaller{}

	t.Cleanup(cluster.SetCSIInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	cfg := newHetznerCSIDriftContext().ClusterCfg
	cfg.Spec.Cluster.CSI = v1alpha1.CSIDisabled

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{Field: specdiff.CSIValuesField},
	}}
	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)
	require.NoError(t, err)
	assert.Zero(t, inst.installs)
}
