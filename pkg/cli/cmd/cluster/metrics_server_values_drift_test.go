package cluster_test

import (
	"bytes"
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

func newMetricsServerDriftContext() *localregistry.Context {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	cfg.Spec.Cluster.MetricsServer = v1alpha1.MetricsServerEnabled

	return &localregistry.Context{ClusterCfg: cfg}
}

// TestCheckChartValuesDrift_ReportsMetricsServerDrift is ksail#7703: an
// installed metrics-server release whose values differ from the rendered ones
// must surface as an in-place change, probed through the resolved context.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_ReportsMetricsServerDrift(t *testing.T) {
	regCtx := newMetricsServerDriftContext()
	probed := &v1alpha1.Cluster{}

	t.Cleanup(cluster.SetMetricsServerInstallerFactoryForTests(
		func(probeCfg *v1alpha1.Cluster) (installer.Installer, error) {
			probeCfg.DeepCopyInto(probed)

			return &driftingInstaller{drifted: true}, nil
		},
	))

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

	fields := make([]string, 0, len(diff.InPlaceChanges))
	for _, change := range diff.InPlaceChanges {
		fields = append(fields, change.Field)
	}

	assert.Equal(t, []string{specdiff.MetricsServerValuesField}, fields)
	assert.Empty(t, stderr.String())
	assert.Equal(t, v1alpha1.MetricsServerEnabled, probed.Spec.Cluster.MetricsServer)

	want := cluster.ExportResolveKubeContext(regCtx)
	assert.Equal(t, want, probed.Spec.Cluster.Connection.Context,
		"the probe must use the resolved kube context")
	assert.NotEmpty(t, probed.Spec.Cluster.Connection.Context)
	assert.Empty(t, regCtx.ClusterCfg.Spec.Cluster.Connection.Context,
		"pinning the probe context must not mutate the caller's config")
}

// TestCheckChartValuesDrift_StaysSilentForMatchingMetricsServer is the control.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_StaysSilentForMatchingMetricsServer(t *testing.T) {
	probes := 0

	t.Cleanup(cluster.SetMetricsServerInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			probes++

			return &driftingInstaller{}, nil
		},
	))

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newMetricsServerDriftContext(), diff)

	assert.Equal(t, 1, probes, "the control must have compared the release")
	assert.Zero(t, diff.TotalChanges())
}

// TestCheckChartValuesDrift_SkipsMetricsServerKSailDoesNotInstall never probes
// a metrics-server the desired configuration does not have KSail install:
// there is no upgrade to schedule, and a release there belongs to someone else
// (K3s ships its own).
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_SkipsMetricsServerKSailDoesNotInstall(t *testing.T) {
	for name, configure := range map[string]func(*v1alpha1.Cluster){
		"disabled": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.MetricsServer = v1alpha1.MetricsServerDisabled
		},
		"default": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.MetricsServer = v1alpha1.MetricsServerDefault
		},
		"distribution ships its own": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionK3s
		},
	} {
		t.Run(name, func(t *testing.T) {
			probes := 0

			t.Cleanup(cluster.SetMetricsServerInstallerFactoryForTests(
				func(*v1alpha1.Cluster) (installer.Installer, error) {
					probes++

					return &driftingInstaller{drifted: true}, nil
				},
			))

			regCtx := newMetricsServerDriftContext()
			configure(regCtx.ClusterCfg)

			cmd, stderr := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			assert.Zero(t, probes, "metrics-server must not be probed")
			assert.Zero(t, diff.TotalChanges())
			assert.Empty(t, stderr.String(), "a component nobody configured must not warn")
		})
	}
}

// TestCheckChartValuesDrift_WarnsWhenMetricsServerCannotBeCompared never turns
// an unreadable release into a clean verdict silently: the update continues,
// but the operator is told the comparison did not happen.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_WarnsWhenMetricsServerCannotBeCompared(t *testing.T) {
	t.Cleanup(cluster.SetMetricsServerInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			return &driftingInstaller{err: errValuesProbeFailed}, nil
		},
	))

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newMetricsServerDriftContext(), diff)

	assert.Zero(t, diff.TotalChanges())
	assert.Contains(t, stderr.String(), "Cannot compare metrics-server values")
}

// TestReconcileMetricsServerValuesSharesOneUpgrade coalesces a
// cluster.metricsServer change and metrics-server values drift in one pass into
// one Helm upgrade, and replays its outcome for both fields.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileMetricsServerValuesSharesOneUpgrade(t *testing.T) {
	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{
			Field:    "cluster.metricsServer",
			OldValue: string(v1alpha1.MetricsServerDefault),
			NewValue: string(v1alpha1.MetricsServerEnabled),
		},
		{Field: specdiff.MetricsServerValuesField},
	}}
	cfg := newMetricsServerDriftContext().ClusterCfg

	t.Run("success", func(t *testing.T) {
		inst := &driftingInstaller{}

		t.Cleanup(cluster.SetMetricsServerInstallerFactoryForTests(
			func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
		))

		result := clusterupdate.NewEmptyUpdateResult()
		err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)
		require.NoError(t, err)
		assert.Equal(t, 1, inst.installs, "both fields share one metrics-server upgrade")
		assert.Len(t, result.AppliedChanges, 2)
	})

	t.Run("failure", func(t *testing.T) {
		builds := 0

		t.Cleanup(cluster.SetMetricsServerInstallerFactoryForTests(
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

// TestReconcileMetricsServerValues_UpgradesOnValuesOnlyDrift applies drift on
// its own: an unchanged ksail.yaml still upgrades the drifted release once.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileMetricsServerValues_UpgradesOnValuesOnlyDrift(t *testing.T) {
	inst := &driftingInstaller{drifted: true}

	t.Cleanup(cluster.SetMetricsServerInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	regCtx := newMetricsServerDriftContext()
	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

	require.Len(t, diff.InPlaceChanges, 1, "the values-only change must be reported")

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(cmd, regCtx.ClusterCfg, diff, result)
	require.NoError(t, err)
	assert.Equal(t, 1, inst.installs)
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, specdiff.MetricsServerValuesField, result.AppliedChanges[0].Field)
}

// TestReconcileMetricsServerValues_SkipsWhenKSailDoesNotInstallIt re-checks the
// configuration at apply time, so a stale drift change never installs a
// metrics-server the configuration does not want.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileMetricsServerValues_SkipsWhenKSailDoesNotInstallIt(t *testing.T) {
	inst := &driftingInstaller{}

	t.Cleanup(cluster.SetMetricsServerInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	cfg := newMetricsServerDriftContext().ClusterCfg
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionK3s

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{Field: specdiff.MetricsServerValuesField},
	}}
	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)
	require.NoError(t, err)
	assert.Zero(t, inst.installs)
}

// TestMetricsServerValuesOnlyUpdateDoesNotRecreateTheCluster covers provisioners
// without an Updater (VCluster): a values-only change is one Helm upgrade, so it
// must be applied in place. Sending it to the recreation flow would delete the
// cluster to change a setting.
//
//nolint:paralleltest // overrides the process-wide installer factories and HOME.
func TestMetricsServerValuesOnlyUpdateDoesNotRecreateTheCluster(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	inst := &driftingInstaller{drifted: true}

	t.Cleanup(cluster.SetMetricsServerInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	regCtx := newMetricsServerDriftContext()
	regCtx.ClusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionVCluster

	cmd, _ := newDriftCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)

	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)
	require.Len(t, diff.InPlaceChanges, 1, "the values-only change must be reported")

	require.NoError(t, cluster.ExportApplySpecOnlyDiff(cmd, regCtx, "values-only", diff, true))
	require.NotContains(t, output.String(), "recreation would be required")
	require.Zero(t, inst.installs, "dry run must not upgrade the release")

	output.Reset()
	require.NoError(t, cluster.ExportApplySpecOnlyDiff(cmd, regCtx, "values-only", diff, false))
	require.Equal(t, 1, inst.installs)
	require.NotContains(t, output.String(), "recreat")
}

// TestValuesOnlyUpdateStillRecreatesForOtherChanges keeps the recreation path for
// every diff that is not made only of chart-values changes.
func TestValuesOnlyUpdateStillRecreatesForOtherChanges(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		edit func(diff *clusterupdate.UpdateResult)
	}{
		{name: "unknown baseline", edit: func(diff *clusterupdate.UpdateResult) {
			diff.UnknownBaseline = []clusterupdate.Change{{Field: "cluster.csi"}}
		}},
		{name: "component switch", edit: func(diff *clusterupdate.UpdateResult) {
			diff.InPlaceChanges = append(
				diff.InPlaceChanges, clusterupdate.Change{Field: "cluster.metricsServer"},
			)
		}},
		{name: "recreate-required change", edit: func(diff *clusterupdate.UpdateResult) {
			diff.RecreateRequired = []clusterupdate.Change{{Field: "cluster.distribution"}}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd, _ := newDriftCmd()

			var output bytes.Buffer
			cmd.SetOut(&output)

			diff := clusterupdate.NewEmptyUpdateResult()
			diff.InPlaceChanges = []clusterupdate.Change{
				{Field: specdiff.MetricsServerValuesField},
				{Field: specdiff.CertManagerValuesField},
			}
			testCase.edit(diff)

			require.NoError(t, cluster.ExportApplySpecOnlyDiff(
				cmd, newMetricsServerDriftContext(), "other-change", diff, true,
			))
			require.Contains(t, output.String(), "recreation would be required")
		})
	}
}
