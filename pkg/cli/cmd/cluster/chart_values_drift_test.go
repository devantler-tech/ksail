package cluster_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	certmanagerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/certmanager"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errValuesProbeFailed = errors.New("values probe failed")

// driftingInstaller is an installer whose rendered-values comparison returns a
// fixed verdict, recording each Install.
type driftingInstaller struct {
	drifted  bool
	err      error
	installs int
}

func (d *driftingInstaller) Install(context.Context) error {
	d.installs++

	return nil
}

func (*driftingInstaller) Uninstall(context.Context) error { return nil }

func (*driftingInstaller) Images(context.Context) ([]string, error) { return nil, nil }

func (d *driftingInstaller) ValuesDrifted(context.Context) (bool, error) {
	return d.drifted, d.err
}

func newCertManagerDriftContext() *localregistry.Context {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	cfg.Spec.Cluster.CertManager = v1alpha1.CertManagerEnabled

	return &localregistry.Context{ClusterCfg: cfg}
}

func newDriftCmd() (*cobra.Command, *bytes.Buffer) {
	var stderr bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetErr(&stderr)

	return cmd, &stderr
}

// TestCheckChartValuesDrift_ReportsCertManagerDrift is ksail#7444: an
// installed cert-manager release whose values differ from the rendered ones
// must surface as an in-place change, probed through the resolved context.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_ReportsCertManagerDrift(t *testing.T) {
	regCtx := newCertManagerDriftContext()
	want := cluster.ExportResolveKubeContext(regCtx)
	require.NotEmpty(t, want, "the test needs a derived context to compare against")

	var probed string

	t.Cleanup(cluster.SetCertManagerInstallerFactoryForTests(
		func(probeCfg *v1alpha1.Cluster) (installer.Installer, error) {
			probed = probeCfg.Spec.Cluster.Connection.Context

			return &driftingInstaller{drifted: true}, nil
		},
	))

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

	require.Len(t, diff.InPlaceChanges, 1)
	assert.Equal(t, specdiff.CertManagerValuesField, diff.InPlaceChanges[0].Field)
	assert.Equal(t, want, probed, "the probe must use the resolved kube context")
	assert.Empty(t, regCtx.ClusterCfg.Spec.Cluster.Connection.Context,
		"pinning the probe context must not mutate the caller's config")
}

// TestCheckChartValuesDrift_StaysSilentForMatchingCertManager is the control.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_StaysSilentForMatchingCertManager(t *testing.T) {
	t.Cleanup(cluster.SetCertManagerInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) {
			return &driftingInstaller{}, nil
		},
	))

	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, newCertManagerDriftContext(), diff)

	assert.Zero(t, diff.TotalChanges())
}

// TestCheckChartValuesDrift_SkipsCertManagerKSailDoesNotInstall never probes a
// cert-manager the desired configuration does not have KSail install: there is
// no upgrade to schedule, and a release there belongs to someone else.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_SkipsCertManagerKSailDoesNotInstall(t *testing.T) {
	for name, configure := range map[string]func(*v1alpha1.Cluster){
		"disabled": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.CertManager = v1alpha1.CertManagerDisabled
		},
		"unset": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.CertManager = ""
		},
		"KWOK": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionKWOK
		},
	} {
		t.Run(name, func(t *testing.T) {
			probes := 0

			t.Cleanup(cluster.SetCertManagerInstallerFactoryForTests(
				func(*v1alpha1.Cluster) (installer.Installer, error) {
					probes++

					return &driftingInstaller{drifted: true}, nil
				},
			))

			regCtx := newCertManagerDriftContext()
			configure(regCtx.ClusterCfg)

			cmd, _ := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			assert.Zero(t, probes, "cert-manager must not be probed")
			assert.Zero(t, diff.TotalChanges())
		})
	}
}

// TestCheckChartValuesDrift_WarnsWhenCertManagerCannotBeCompared never turns
// an unreadable release into a clean verdict silently: the update continues,
// but the operator is told the comparison did not happen.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_WarnsWhenCertManagerCannotBeCompared(t *testing.T) {
	for name, testCase := range map[string]struct {
		inst     installer.Installer
		buildErr error
	}{
		"installer cannot be built": {buildErr: errValuesProbeFailed},
		"release cannot be read": {
			inst: &driftingInstaller{drifted: true, err: errValuesProbeFailed},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(cluster.SetCertManagerInstallerFactoryForTests(
				func(*v1alpha1.Cluster) (installer.Installer, error) {
					return testCase.inst, testCase.buildErr
				},
			))

			cmd, stderr := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, newCertManagerDriftContext(), diff)

			assert.Zero(t, diff.TotalChanges(), "a failed probe must not report drift")
			assert.Contains(t, stderr.String(), "cert-manager")
			assert.Contains(t, stderr.String(), errValuesProbeFailed.Error())
		})
	}
}

// TestChartValuesDriftFieldsReconcileInPlace pins detection to application for
// every probed component: a field with no handler would be detected and then
// skipped, and one outside the component-reconcile set would be demoted to
// "recreate required" by a provisioner that declares its in-place fields.
func TestChartValuesDriftFieldsReconcileInPlace(t *testing.T) {
	t.Parallel()

	fields := cluster.ExportChartValuesDriftFields()
	require.Contains(t, fields, specdiff.AutoscalerValuesField)
	require.Contains(t, fields, specdiff.CertManagerValuesField)

	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			assert.True(t,
				cluster.ExportHandlerForField(&cobra.Command{}, &v1alpha1.Cluster{}, field),
				"no reconcile handler is registered for %q", field)

			diff := clusterupdate.NewEmptyUpdateResult()
			diff.InPlaceChanges = []clusterupdate.Change{
				{Field: field, Category: clusterupdate.ChangeCategoryInPlace},
			}

			cluster.ExportPromoteUnsupportedInPlaceChanges(&fieldSupportUpdater{
				fakeUpdater: &fakeUpdater{},
				supported:   map[string]bool{},
			}, diff)

			assert.Empty(
				t,
				diff.RecreateRequired,
				"a values-only upgrade must never demand recreation",
			)
			assert.Len(t, diff.InPlaceChanges, 1)
		})
	}
}

// TestReconcileCertManagerValuesSharesOneUpgrade coalesces a cluster.certManager
// change and cert-manager values drift in one pass into one Helm upgrade, and
// replays its outcome for both fields.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileCertManagerValuesSharesOneUpgrade(t *testing.T) {
	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{
			Field:    "cluster.certManager",
			OldValue: string(v1alpha1.CertManagerDisabled),
			NewValue: string(v1alpha1.CertManagerEnabled),
		},
		{Field: specdiff.CertManagerValuesField},
	}}

	t.Run("success", func(t *testing.T) {
		inst := &driftingInstaller{}

		t.Cleanup(cluster.SetCertManagerInstallerFactoryForTests(
			func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
		))

		result := clusterupdate.NewEmptyUpdateResult()
		err := cluster.ExportReconcileComponents(
			newReconcileTestCmd(), newCertManagerDriftContext().ClusterCfg, diff, result,
		)
		require.NoError(t, err)
		assert.Equal(t, 1, inst.installs, "both fields share one cert-manager upgrade")
		assert.Len(t, result.AppliedChanges, 2)
	})

	t.Run("failure", func(t *testing.T) {
		builds := 0

		t.Cleanup(cluster.SetCertManagerInstallerFactoryForTests(
			func(*v1alpha1.Cluster) (installer.Installer, error) {
				builds++

				return nil, errValuesProbeFailed
			},
		))

		result := clusterupdate.NewEmptyUpdateResult()
		err := cluster.ExportReconcileComponents(
			newReconcileTestCmd(), newCertManagerDriftContext().ClusterCfg, diff, result,
		)
		require.ErrorIs(t, err, errValuesProbeFailed)
		assert.Equal(t, 1, builds, "the failed upgrade must not be retried per field")
		assert.Len(t, result.FailedChanges, 2, "the first failure must survive deduplication")
	})
}

// certManagerHelmRelease wires the real cert-manager installer to a fake Helm
// client holding a deployed release with the given values.
func certManagerHelmRelease(
	t *testing.T,
	deployed func(rendered map[string]any),
) *helm.MockInterface {
	t.Helper()

	client := helm.NewMockInterface(t)
	certManager := certmanagerinstaller.NewInstaller(client, 5*time.Minute, false)

	rendered, err := certManager.RenderedValues()
	require.NoError(t, err)

	raw, err := json.Marshal(rendered)
	require.NoError(t, err)

	var stored map[string]any

	require.NoError(t, json.Unmarshal(raw, &stored))
	deployed(stored)

	client.EXPECT().
		ReleaseExists(mock.Anything, "cert-manager", "cert-manager").
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "cert-manager", "cert-manager").
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().
		GetReleaseValues(mock.Anything, "cert-manager", "cert-manager").
		Return(stored, nil)

	t.Cleanup(cluster.SetCertManagerInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return certManager, nil },
	))

	return client
}

// TestUpdatePath_UpgradesCertManagerOnValuesOnlyDrift drives detection and
// reconciliation together with the real cert-manager installer and a fake Helm
// client: a release installed by an older KSail (here, without the
// startupapicheck timeout this KSail renders) is upgraded once, while an
// unchanged ksail.yaml produces no spec diff at all.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestUpdatePath_UpgradesCertManagerOnValuesOnlyDrift(t *testing.T) {
	client := certManagerHelmRelease(t, func(stored map[string]any) {
		delete(stored, "startupapicheck")
	})

	var upgraded *helm.ChartSpec

	client.EXPECT().AddRepository(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, spec *helm.ChartSpec) (*helm.ReleaseInfo, error) {
			upgraded = spec

			return &helm.ReleaseInfo{}, nil
		}).
		Once()

	regCtx := newCertManagerDriftContext()
	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

	require.Len(t, diff.InPlaceChanges, 1, "the values-only change must be reported")

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(cmd, regCtx.ClusterCfg, diff, result)
	require.NoError(t, err)

	require.NotNil(t, upgraded, "the drifted release must be upgraded")
	assert.Equal(t, "cert-manager", upgraded.ReleaseName)
	assert.Contains(t, upgraded.SetValues, "startupapicheck.timeout")
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, specdiff.CertManagerValuesField, result.AppliedChanges[0].Field)
}

// TestUpdatePath_LeavesMatchingCertManagerAlone is the control for the test
// above: the same release carrying the rendered values is never upgraded (the
// fake Helm client fails the test on any unexpected install).
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestUpdatePath_LeavesMatchingCertManagerAlone(t *testing.T) {
	certManagerHelmRelease(t, func(map[string]any) {})

	regCtx := newCertManagerDriftContext()
	cmd, _ := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

	require.Zero(t, diff.TotalChanges())

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(cmd, regCtx.ClusterCfg, diff, result)
	require.NoError(t, err)
	assert.Empty(t, result.AppliedChanges)
}
