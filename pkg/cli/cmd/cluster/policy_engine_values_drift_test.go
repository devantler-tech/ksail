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

func newPolicyEngineDriftContext(engine v1alpha1.PolicyEngine) *localregistry.Context {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	cfg.Spec.Cluster.PolicyEngine = engine

	return &localregistry.Context{ClusterCfg: cfg}
}

// TestCheckChartValuesDrift_ReportsPolicyEngineDrift is ksail#7651: an
// installed Kyverno or Gatekeeper release whose values differ from the
// rendered ones must surface as an in-place change, probed through the resolved
// context with the engine the configuration selects.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_ReportsPolicyEngineDrift(t *testing.T) {
	for _, engine := range []v1alpha1.PolicyEngine{
		v1alpha1.PolicyEngineKyverno,
		v1alpha1.PolicyEngineGatekeeper,
	} {
		t.Run(string(engine), func(t *testing.T) {
			regCtx := newPolicyEngineDriftContext(engine)
			want := cluster.ExportResolveKubeContext(regCtx)
			require.NotEmpty(t, want, "the test needs a derived context to compare against")

			var probed v1alpha1.Cluster

			t.Cleanup(cluster.SetPolicyEngineInstallerFactoryForTests(
				func(probeCfg *v1alpha1.Cluster) (installer.Installer, error) {
					probed = *probeCfg

					return &driftingInstaller{drifted: true}, nil
				},
			))

			cmd, _ := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			require.Len(t, diff.InPlaceChanges, 1)
			assert.Equal(t, specdiff.PolicyEngineValuesField, diff.InPlaceChanges[0].Field)
			assert.Equal(t, engine, probed.Spec.Cluster.PolicyEngine)
			assert.Equal(t, want, probed.Spec.Cluster.Connection.Context,
				"the probe must use the resolved kube context")
			assert.Empty(t, regCtx.ClusterCfg.Spec.Cluster.Connection.Context,
				"pinning the probe context must not mutate the caller's config")
		})
	}
}

// TestCheckChartValuesDrift_StaysSilentForMatchingPolicyEngine is the control.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_StaysSilentForMatchingPolicyEngine(t *testing.T) {
	probes := 0

	t.Cleanup(cluster.SetPolicyEngineInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			probes++

			return &driftingInstaller{}, nil
		},
	))

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(
		cmd, newPolicyEngineDriftContext(v1alpha1.PolicyEngineKyverno), diff,
	)

	assert.Equal(t, 1, probes, "the control must have compared the release")
	assert.Zero(t, diff.TotalChanges())
}

// TestCheckChartValuesDrift_SkipsPolicyEngineKSailDoesNotInstall never probes
// a policy engine the desired configuration does not have KSail install: there
// is no upgrade to schedule, and a release there belongs to someone else.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_SkipsPolicyEngineKSailDoesNotInstall(t *testing.T) {
	for name, configure := range map[string]func(*v1alpha1.Cluster){
		"none": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.PolicyEngine = v1alpha1.PolicyEngineNone
		},
		"unset": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.PolicyEngine = ""
		},
		"KWOK": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionKWOK
		},
	} {
		t.Run(name, func(t *testing.T) {
			probes := 0

			t.Cleanup(cluster.SetPolicyEngineInstallerFactoryForTests(
				func(*v1alpha1.Cluster) (installer.Installer, error) {
					probes++

					return &driftingInstaller{drifted: true}, nil
				},
			))

			regCtx := newPolicyEngineDriftContext(v1alpha1.PolicyEngineKyverno)
			configure(regCtx.ClusterCfg)

			cmd, stderr := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			assert.Zero(t, probes, "the policy engine must not be probed")
			assert.Zero(t, diff.TotalChanges())
			assert.Empty(t, stderr.String(), "an engine nobody configured must not warn")
		})
	}
}

// TestCheckChartValuesDrift_WarnsWhenPolicyEngineCannotBeCompared never turns
// an unreadable release into a clean verdict silently: the update continues,
// but the operator is told the comparison did not happen.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_WarnsWhenPolicyEngineCannotBeCompared(t *testing.T) {
	t.Cleanup(cluster.SetPolicyEngineInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			return &driftingInstaller{err: errValuesProbeFailed}, nil
		},
	))

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(
		cmd, newPolicyEngineDriftContext(v1alpha1.PolicyEngineGatekeeper), diff,
	)

	assert.Zero(t, diff.TotalChanges())
	assert.Contains(t, stderr.String(), "Cannot compare policy-engine values")
}

// TestReconcilePolicyEngineValuesSharesOneUpgrade coalesces a
// cluster.policyEngine change and policy-engine values drift in one pass into
// one Helm upgrade, and replays its outcome for both fields.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcilePolicyEngineValuesSharesOneUpgrade(t *testing.T) {
	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{
			Field:    "cluster.policyEngine",
			OldValue: string(v1alpha1.PolicyEngineNone),
			NewValue: string(v1alpha1.PolicyEngineKyverno),
		},
		{Field: specdiff.PolicyEngineValuesField},
	}}
	cfg := newPolicyEngineDriftContext(v1alpha1.PolicyEngineKyverno).ClusterCfg

	t.Run("success", func(t *testing.T) {
		inst := &driftingInstaller{}

		t.Cleanup(cluster.SetPolicyEngineInstallerFactoryForTests(
			func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
		))

		result := clusterupdate.NewEmptyUpdateResult()
		err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)
		require.NoError(t, err)
		assert.Equal(t, 1, inst.installs, "both fields share one policy-engine upgrade")
		assert.Len(t, result.AppliedChanges, 2)
	})

	t.Run("failure", func(t *testing.T) {
		builds := 0

		t.Cleanup(cluster.SetPolicyEngineInstallerFactoryForTests(
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

// TestReconcilePolicyEngineValues_UpgradesOnValuesOnlyDrift applies drift on
// its own: an unchanged ksail.yaml still upgrades the drifted release once.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcilePolicyEngineValues_UpgradesOnValuesOnlyDrift(t *testing.T) {
	inst := &driftingInstaller{drifted: true}

	t.Cleanup(cluster.SetPolicyEngineInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	regCtx := newPolicyEngineDriftContext(v1alpha1.PolicyEngineGatekeeper)
	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

	require.Len(t, diff.InPlaceChanges, 1, "the values-only change must be reported")

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(cmd, regCtx.ClusterCfg, diff, result)
	require.NoError(t, err)
	assert.Equal(t, 1, inst.installs)
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, specdiff.PolicyEngineValuesField, result.AppliedChanges[0].Field)
}

// TestReconcilePolicyEngineValues_SkipsAnEngineKSailDoesNotInstall re-checks
// the configuration at apply time, so a stale drift change never installs an
// engine the configuration does not want.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcilePolicyEngineValues_SkipsAnEngineKSailDoesNotInstall(t *testing.T) {
	inst := &driftingInstaller{}

	t.Cleanup(cluster.SetPolicyEngineInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{Field: specdiff.PolicyEngineValuesField},
	}}
	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(),
		newPolicyEngineDriftContext(v1alpha1.PolicyEngineNone).ClusterCfg,
		diff, result,
	)
	require.NoError(t, err)
	assert.Zero(t, inst.installs)
}
