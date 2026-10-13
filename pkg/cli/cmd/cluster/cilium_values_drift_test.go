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

func newCiliumDriftContext() *localregistry.Context {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	cfg.Spec.Cluster.CNI = v1alpha1.CNICilium

	return &localregistry.Context{ClusterCfg: cfg}
}

func changedFields(diff *clusterupdate.UpdateResult) []string {
	fields := make([]string, 0, len(diff.InPlaceChanges))
	for _, change := range diff.InPlaceChanges {
		fields = append(fields, change.Field)
	}

	return fields
}

// TestCheckChartValuesDrift_ReportsCiliumDrift is ksail#7722: an installed
// Cilium release whose values differ from the rendered ones must surface as an
// in-place change, probed through the resolved context.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_ReportsCiliumDrift(t *testing.T) {
	regCtx := newCiliumDriftContext()
	probed := &v1alpha1.Cluster{}

	t.Cleanup(cluster.SetCiliumInstallerFactoryForTests(
		func(probeCfg *v1alpha1.Cluster) (installer.Installer, error) {
			probeCfg.DeepCopyInto(probed)

			return &driftingInstaller{drifted: true}, nil
		},
	))

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

	assert.Equal(t, []string{specdiff.CiliumValuesField}, changedFields(diff))
	assert.Empty(t, stderr.String())
	assert.Equal(t, v1alpha1.CNICilium, probed.Spec.Cluster.CNI)

	want := cluster.ExportResolveKubeContext(regCtx)
	assert.Equal(t, want, probed.Spec.Cluster.Connection.Context,
		"the probe must use the resolved kube context")
	assert.NotEmpty(t, probed.Spec.Cluster.Connection.Context)
	assert.Empty(t, regCtx.ClusterCfg.Spec.Cluster.Connection.Context,
		"pinning the probe context must not mutate the caller's config")
}

// TestCheckChartValuesDrift_StaysSilentForMatchingCilium is the control.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_StaysSilentForMatchingCilium(t *testing.T) {
	probes := 0

	t.Cleanup(cluster.SetCiliumInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			probes++

			return &driftingInstaller{}, nil
		},
	))

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newCiliumDriftContext(), diff)

	assert.Equal(t, 1, probes, "the control must have compared the release")
	assert.Zero(t, diff.TotalChanges())
}

// TestCheckChartValuesDrift_SkipsCiliumKSailDoesNotInstall never probes Cilium
// when the desired configuration does not have KSail install it: there is no
// upgrade to schedule, and a release there belongs to someone else.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_SkipsCiliumKSailDoesNotInstall(t *testing.T) {
	for name, configure := range map[string]func(*v1alpha1.Cluster){
		"default CNI": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.CNI = v1alpha1.CNIDefault
		},
		"unset CNI": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.CNI = ""
		},
		"distribution without a CNI": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionKWOK
		},
	} {
		t.Run(name, func(t *testing.T) {
			probes := 0

			t.Cleanup(cluster.SetCiliumInstallerFactoryForTests(
				func(*v1alpha1.Cluster) (installer.Installer, error) {
					probes++

					return &driftingInstaller{drifted: true}, nil
				},
			))

			regCtx := newCiliumDriftContext()
			configure(regCtx.ClusterCfg)

			cmd, stderr := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			assert.Zero(t, probes, "Cilium must not be probed")
			assert.Zero(t, diff.TotalChanges())
			assert.Empty(t, stderr.String(), "a component nobody configured must not warn")
		})
	}
}

// TestCheckChartValuesDrift_LeavesCiliumToACNIChange does not compare values
// beside a cluster.cni change: that change installs the CNI itself, so a second
// upgrade of the same release in one pass would only repeat it.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_LeavesCiliumToACNIChange(t *testing.T) {
	probes := 0

	t.Cleanup(cluster.SetCiliumInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			probes++

			return &driftingInstaller{drifted: true}, nil
		},
	))

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = []clusterupdate.Change{{Field: specdiff.CNIField}}
	cluster.ExportCheckChartValuesDrift(cmd, newCiliumDriftContext(), diff)

	assert.Zero(t, probes, "the CNI change already installs Cilium")
	assert.Equal(t, []string{specdiff.CNIField}, changedFields(diff))
}

// TestCheckChartValuesDrift_WarnsWhenCiliumCannotBeCompared never turns an
// unreadable release into a clean verdict silently: the update continues, but
// the operator is told the comparison did not happen.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_WarnsWhenCiliumCannotBeCompared(t *testing.T) {
	t.Cleanup(cluster.SetCiliumInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			return &driftingInstaller{err: errValuesProbeFailed}, nil
		},
	))

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newCiliumDriftContext(), diff)

	assert.Zero(t, diff.TotalChanges())
	assert.Contains(t, stderr.String(), "Cannot compare cilium values")
}

// TestReconcileCiliumValues_UpgradesOnValuesOnlyDrift applies drift on its own:
// an unchanged ksail.yaml still upgrades the drifted release once, through the
// installer the probe compared with.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileCiliumValues_UpgradesOnValuesOnlyDrift(t *testing.T) {
	inst := &driftingInstaller{drifted: true}

	t.Cleanup(cluster.SetCiliumInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	regCtx := newCiliumDriftContext()
	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

	require.Len(t, diff.InPlaceChanges, 1, "the values-only change must be reported")

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(cmd, regCtx.ClusterCfg, diff, result)
	require.NoError(t, err)
	assert.Equal(t, 1, inst.installs)
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, specdiff.CiliumValuesField, result.AppliedChanges[0].Field)
}

// TestReconcileCiliumValues_ReportsAFailedUpgrade keeps a failed upgrade
// visible as a failed change instead of an applied one.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileCiliumValues_ReportsAFailedUpgrade(t *testing.T) {
	t.Cleanup(cluster.SetCiliumInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			return nil, errValuesProbeFailed
		},
	))

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{Field: specdiff.CiliumValuesField},
	}}
	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), newCiliumDriftContext().ClusterCfg, diff, result,
	)
	require.ErrorIs(t, err, errValuesProbeFailed)
	assert.Empty(t, result.AppliedChanges)
	require.Len(t, result.FailedChanges, 1)
	assert.Equal(t, specdiff.CiliumValuesField, result.FailedChanges[0].Field)
}

// TestReconcileCiliumValues_SkipsWhenKSailDoesNotInstallIt re-checks the
// configuration at apply time, so a stale drift change never installs a CNI
// the configuration does not want.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileCiliumValues_SkipsWhenKSailDoesNotInstallIt(t *testing.T) {
	inst := &driftingInstaller{}

	t.Cleanup(cluster.SetCiliumInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	cfg := newCiliumDriftContext().ClusterCfg
	cfg.Spec.Cluster.CNI = v1alpha1.CNIDefault

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{Field: specdiff.CiliumValuesField},
	}}
	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)
	require.NoError(t, err)
	assert.Zero(t, inst.installs)
}
