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

// newHcloudCCMDriftContext is a Talos cluster on Hetzner, which always has
// KSail install the Hetzner cloud controller manager. The storage driver is
// disabled so only the controller is compared.
func newHcloudCCMDriftContext() *localregistry.Context {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
	cfg.Spec.Cluster.CSI = v1alpha1.CSIDisabled

	return &localregistry.Context{ClusterCfg: cfg}
}

// TestCheckChartValuesDrift_ReportsHcloudCCMDrift is ksail#7366: an installed
// cloud controller release whose values differ from the rendered ones must
// surface as an in-place change, probed through the resolved context.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_ReportsHcloudCCMDrift(t *testing.T) {
	probed := assertReportsValuesDrift(t, newHcloudCCMDriftContext(),
		cluster.SetHcloudCCMInstallerFactoryForTests, specdiff.HcloudCCMValuesField)

	assert.Equal(t, v1alpha1.ProviderHetzner, probed.Spec.Cluster.Provider)
}

// TestCheckChartValuesDrift_StaysSilentForMatchingHcloudCCM is the control.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_StaysSilentForMatchingHcloudCCM(t *testing.T) {
	probes := 0

	t.Cleanup(cluster.SetHcloudCCMInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			probes++

			return &driftingInstaller{}, nil
		},
	))

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newHcloudCCMDriftContext(), diff)

	assert.Equal(t, 1, probes, "the control must have compared the release")
	assert.Zero(t, diff.TotalChanges())
}

// TestCheckChartValuesDrift_SkipsHcloudCCMOffHetzner never probes a cloud
// controller KSail does not install: only Talos on Hetzner gets hcloud-ccm.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_SkipsHcloudCCMOffHetzner(t *testing.T) {
	for name, configure := range map[string]func(*v1alpha1.Cluster){
		"talos on docker": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
		},
		"talos on omni": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Provider = v1alpha1.ProviderOmni
		},
		"vanilla on docker": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
			cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
		},
	} {
		t.Run(name, func(t *testing.T) {
			probes := 0

			t.Cleanup(cluster.SetHcloudCCMInstallerFactoryForTests(
				func(*v1alpha1.Cluster) (installer.Installer, error) {
					probes++

					return &driftingInstaller{drifted: true}, nil
				},
			))

			regCtx := newHcloudCCMDriftContext()
			configure(regCtx.ClusterCfg)

			cmd, _ := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			assert.Zero(t, probes, "the cloud controller must not be probed")

			for _, change := range diff.InPlaceChanges {
				assert.NotEqual(t, specdiff.HcloudCCMValuesField, change.Field)
			}
		})
	}
}

// TestCheckChartValuesDrift_WarnsWhenHcloudCCMCannotBeCompared never turns an
// unreadable release into a clean verdict silently: the update continues, but
// the operator is told the comparison did not happen.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_WarnsWhenHcloudCCMCannotBeCompared(t *testing.T) {
	t.Cleanup(cluster.SetHcloudCCMInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			return &driftingInstaller{err: errValuesProbeFailed}, nil
		},
	))

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newHcloudCCMDriftContext(), diff)

	assert.Zero(t, diff.TotalChanges())
	assert.Contains(t, stderr.String(), "Cannot compare hcloud-ccm values")
}

// TestReconcileHcloudCCMValuesSharesOneUpgrade coalesces a cluster.loadBalancer
// change and cloud controller values drift in one pass into one Helm upgrade,
// and replays its outcome for both fields.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileHcloudCCMValuesSharesOneUpgrade(t *testing.T) {
	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{
			Field:    "cluster.loadBalancer",
			OldValue: string(v1alpha1.LoadBalancerDefault),
			NewValue: string(v1alpha1.LoadBalancerEnabled),
		},
		{Field: specdiff.HcloudCCMValuesField},
	}}
	cfg := newHcloudCCMDriftContext().ClusterCfg
	cfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled

	t.Run("success", func(t *testing.T) {
		inst := &driftingInstaller{}

		t.Cleanup(cluster.SetHcloudCCMInstallerFactoryForTests(
			func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
		))

		result := clusterupdate.NewEmptyUpdateResult()
		err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)
		require.NoError(t, err)
		assert.Equal(t, 1, inst.installs, "both fields share one cloud controller upgrade")
		assert.Len(t, result.AppliedChanges, 2)
	})

	t.Run("failure", func(t *testing.T) {
		builds := 0

		t.Cleanup(cluster.SetHcloudCCMInstallerFactoryForTests(
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

// TestReconcileHcloudCCMValues_UpgradesOnValuesOnlyDrift applies drift on its
// own: an unchanged ksail.yaml still upgrades the drifted release once. With
// load balancers disabled this is also what switches the controller's service
// controller off, which a cluster.loadBalancer change alone never upgrades.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileHcloudCCMValues_UpgradesOnValuesOnlyDrift(t *testing.T) {
	for name, setting := range map[string]v1alpha1.LoadBalancer{
		"default":  v1alpha1.LoadBalancerDefault,
		"disabled": v1alpha1.LoadBalancerDisabled,
	} {
		t.Run(name, func(t *testing.T) {
			inst := &driftingInstaller{drifted: true}

			t.Cleanup(cluster.SetHcloudCCMInstallerFactoryForTests(
				func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
			))

			regCtx := newHcloudCCMDriftContext()
			regCtx.ClusterCfg.Spec.Cluster.LoadBalancer = setting

			cmd, _ := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			require.Len(t, diff.InPlaceChanges, 1, "the values-only change must be reported")

			result := clusterupdate.NewEmptyUpdateResult()
			err := cluster.ExportReconcileComponents(cmd, regCtx.ClusterCfg, diff, result)
			require.NoError(t, err)
			assert.Equal(t, 1, inst.installs)
			require.Len(t, result.AppliedChanges, 1)
			assert.Equal(t, specdiff.HcloudCCMValuesField, result.AppliedChanges[0].Field)
		})
	}
}

// TestReconcileHcloudCCMValues_SkipsWhenKSailDoesNotInstallIt re-checks the
// configuration at apply time, so a stale drift change never installs a cloud
// controller on a cluster that is not Talos on Hetzner.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileHcloudCCMValues_SkipsWhenKSailDoesNotInstallIt(t *testing.T) {
	inst := &driftingInstaller{}

	t.Cleanup(cluster.SetHcloudCCMInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	cfg := newHcloudCCMDriftContext().ClusterCfg
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{Field: specdiff.HcloudCCMValuesField},
	}}
	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)
	require.NoError(t, err)
	assert.Zero(t, inst.installs)
}
