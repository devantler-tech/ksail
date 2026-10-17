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

// newArgoCDDriftContext is a Vanilla cluster with Argo CD as its GitOps
// engine. Metrics-server is disabled so only Argo CD is compared.
func newArgoCDDriftContext() *localregistry.Context {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	cfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineArgoCD
	cfg.Spec.Cluster.MetricsServer = v1alpha1.MetricsServerDisabled

	return &localregistry.Context{ClusterCfg: cfg}
}

// TestCheckChartValuesDrift_ReportsArgoCDDrift is ksail#7772: an installed Argo
// CD release whose values differ from the rendered ones must surface as an
// in-place change, probed through the resolved context.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_ReportsArgoCDDrift(t *testing.T) {
	probed := assertReportsValuesDrift(t, newArgoCDDriftContext(),
		cluster.SetArgoCDInstallerFactoryForTests, specdiff.ArgoCDValuesField)

	assert.Equal(t, v1alpha1.GitOpsEngineArgoCD, probed.Spec.Cluster.GitOpsEngine)
}

// TestCheckChartValuesDrift_StaysSilentForMatchingArgoCD is the control: a
// release that already carries the rendered values is compared and left alone.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_StaysSilentForMatchingArgoCD(t *testing.T) {
	probes := 0

	t.Cleanup(cluster.SetArgoCDInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			probes++

			return &driftingInstaller{}, nil
		},
	))

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newArgoCDDriftContext(), diff)

	assert.Equal(t, 1, probes, "the control must have compared the release")
	assert.Zero(t, diff.TotalChanges())
	assert.Empty(t, stderr.String())
}

// TestCheckChartValuesDrift_SkipsArgoCDForOtherEngines never probes Argo CD
// when the configuration does not have KSail install it.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_SkipsArgoCDForOtherEngines(t *testing.T) {
	for name, engine := range map[string]v1alpha1.GitOpsEngine{
		"flux":  v1alpha1.GitOpsEngineFlux,
		"none":  v1alpha1.GitOpsEngineNone,
		"unset": "",
	} {
		t.Run(name, func(t *testing.T) {
			probes := 0

			t.Cleanup(cluster.SetArgoCDInstallerFactoryForTests(
				func(*v1alpha1.Cluster) (installer.Installer, error) {
					probes++

					return &driftingInstaller{drifted: true}, nil
				},
			))

			regCtx := newArgoCDDriftContext()
			regCtx.ClusterCfg.Spec.Cluster.GitOpsEngine = engine

			cmd, _ := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			assert.Zero(t, probes, "Argo CD must not be probed")

			for _, change := range diff.InPlaceChanges {
				assert.NotEqual(t, specdiff.ArgoCDValuesField, change.Field)
			}
		})
	}
}

// TestCheckChartValuesDrift_WarnsWhenArgoCDCannotBeCompared never turns an
// unreadable release into a clean verdict silently.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_WarnsWhenArgoCDCannotBeCompared(t *testing.T) {
	t.Cleanup(cluster.SetArgoCDInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			return &driftingInstaller{err: errValuesProbeFailed}, nil
		},
	))

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newArgoCDDriftContext(), diff)

	assert.Zero(t, diff.TotalChanges())
	assert.Contains(t, stderr.String(), "Cannot compare argocd values")
}

// TestReconcileArgoCDValuesSharesOneUpgrade coalesces a cluster.gitOpsEngine
// change to ArgoCD and Argo CD values drift in one pass into one Helm upgrade,
// and replays its outcome for both fields.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileArgoCDValuesSharesOneUpgrade(t *testing.T) {
	inst := &driftingInstaller{}

	t.Cleanup(cluster.SetArgoCDInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), newArgoCDDriftContext().ClusterCfg, argoCDEngineAndDrift(), result,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, inst.installs, "both fields share one Argo CD upgrade")
	assert.Len(t, result.AppliedChanges, 2)
}

// TestReconcileArgoCDValuesReplaysOneFailure builds and runs the failed Argo CD
// upgrade once and reports it for both fields.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileArgoCDValuesReplaysOneFailure(t *testing.T) {
	builds := 0

	t.Cleanup(cluster.SetArgoCDInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			builds++

			return nil, errValuesProbeFailed
		},
	))

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), newArgoCDDriftContext().ClusterCfg, argoCDEngineAndDrift(), result,
	)

	require.ErrorIs(t, err, errValuesProbeFailed)
	assert.Equal(t, 1, builds, "the failed upgrade must not be retried per field")
	assert.Len(t, result.FailedChanges, 2, "the first failure must survive deduplication")
}

// TestReconcileArgoCDValues_UpgradesOnValuesOnlyDrift applies drift on its
// own: an unchanged ksail.yaml still upgrades the drifted release once.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileArgoCDValues_UpgradesOnValuesOnlyDrift(t *testing.T) {
	inst := &driftingInstaller{drifted: true}

	t.Cleanup(cluster.SetArgoCDInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	regCtx := newArgoCDDriftContext()

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)
	require.Len(t, diff.InPlaceChanges, 1, stderr.String())

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(newReconcileTestCmd(), regCtx.ClusterCfg, diff, result)

	require.NoError(t, err)
	assert.Equal(t, 1, inst.installs)
	assert.Len(t, result.AppliedChanges, 1)
}

// TestReconcileArgoCDValues_IgnoresStaleDrift never installs Argo CD when the
// desired configuration no longer selects it.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileArgoCDValues_IgnoresStaleDrift(t *testing.T) {
	inst := &driftingInstaller{drifted: true}

	t.Cleanup(cluster.SetArgoCDInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	cfg := newArgoCDDriftContext().ClusterCfg
	cfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{Field: specdiff.ArgoCDValuesField},
	}}

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)

	require.NoError(t, err)
	assert.Zero(t, inst.installs)
}

// argoCDEngineAndDrift is one pass that switches the GitOps engine to ArgoCD
// and also finds Argo CD values drift.
func argoCDEngineAndDrift() *clusterupdate.UpdateResult {
	return &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{
			Field:    "cluster.gitOpsEngine",
			OldValue: string(v1alpha1.GitOpsEngineNone),
			NewValue: string(v1alpha1.GitOpsEngineArgoCD),
		},
		{Field: specdiff.ArgoCDValuesField},
	}}
}
