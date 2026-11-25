package cluster_test

import (
	"context"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	metallbinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metallb"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testLoadBalancerRegion = "eu-north-1"

// driftingEKSLoadBalancerInstaller is the EKS controller fake with a values
// comparison, so one installer serves both the probe and the reconcile.
type driftingEKSLoadBalancerInstaller struct {
	recordingEKSLoadBalancerInstaller

	drifted bool
	err     error
}

func (d *driftingEKSLoadBalancerInstaller) ValuesDrifted(context.Context) (bool, error) {
	return d.drifted, d.err
}

// newMetalLBDriftContext is a Talos cluster on Docker with load balancers
// Enabled, which has KSail install MetalLB. Metrics-server is disabled so only
// the load balancer is compared.
func newMetalLBDriftContext() *localregistry.Context {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	cfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled
	cfg.Spec.Cluster.MetricsServer = v1alpha1.MetricsServerDisabled

	return &localregistry.Context{ClusterCfg: cfg}
}

// newAWSLoadBalancerControllerDriftContext is an EKS cluster that opted into
// the AWS Load Balancer Controller. Metrics-server is disabled so only the load
// balancer is compared.
func newAWSLoadBalancerControllerDriftContext() *localregistry.Context {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	cfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerEnabled
	cfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = true
	cfg.Spec.Cluster.MetricsServer = v1alpha1.MetricsServerDisabled

	return &localregistry.Context{ClusterCfg: cfg}
}

// TestCheckChartValuesDrift_ReportsMetalLBDrift is ksail#7771: an installed
// MetalLB release whose values differ from the rendered ones must surface as
// an in-place change, probed through the resolved context.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_ReportsMetalLBDrift(t *testing.T) {
	assertReportsValuesDrift(t, newMetalLBDriftContext(),
		cluster.SetMetalLBInstallerFactoryForTests, specdiff.MetalLBValuesField)
}

// TestCheckChartValuesDrift_ReportsAWSLoadBalancerControllerDrift is
// ksail#7771 for the EKS controller.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_ReportsAWSLoadBalancerControllerDrift(t *testing.T) {
	probed := assertReportsValuesDrift(t, newAWSLoadBalancerControllerDriftContext(),
		cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests,
		specdiff.AWSLoadBalancerControllerValuesField)

	assert.Equal(t, v1alpha1.DistributionEKS, probed.Spec.Cluster.Distribution)
}

// TestCheckChartValuesDrift_StaysSilentForMatchingLoadBalancers is the
// control: a release that already carries the rendered values is compared and
// left alone.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_StaysSilentForMatchingLoadBalancers(t *testing.T) {
	for name, testCase := range map[string]struct {
		regCtx   *localregistry.Context
		override installerFactoryOverride
	}{
		"metallb": {newMetalLBDriftContext(), cluster.SetMetalLBInstallerFactoryForTests},
		"aws-load-balancer-controller": {
			newAWSLoadBalancerControllerDriftContext(),
			cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests,
		},
	} {
		t.Run(name, func(t *testing.T) {
			probes := 0

			t.Cleanup(testCase.override(func(*v1alpha1.Cluster) (installer.Installer, error) {
				probes++

				return &driftingInstaller{}, nil
			}))

			cmd, stderr := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, testCase.regCtx, diff)

			assert.Equal(t, 1, probes, "the control must have compared the release")
			assert.Zero(t, diff.TotalChanges())
			assert.Empty(t, stderr.String())
		})
	}
}

// TestCheckChartValuesDrift_SkipsLoadBalancersKSailDoesNotInstall never
// probes a load balancer without a KSail Helm release: one that is not
// Enabled, one the distribution provides, and cloud-provider-kind, which runs
// as a container rather than a release.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_SkipsLoadBalancersKSailDoesNotInstall(t *testing.T) {
	for name, configure := range map[string]func(*v1alpha1.Cluster){
		"talos on docker, default": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerDefault
		},
		"talos on docker, disabled": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerDisabled
		},
		"vanilla on docker (cloud-provider-kind)": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
		},
		"k3s on docker": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionK3s
		},
		"eks without the controller opt-in": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
			cfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
		},
		"eks with the opt-in but load balancers default": func(cfg *v1alpha1.Cluster) {
			cfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
			cfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
			cfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerDefault
			cfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			probes := 0
			probe := func(*v1alpha1.Cluster) (installer.Installer, error) {
				probes++

				return &driftingInstaller{drifted: true}, nil
			}

			t.Cleanup(cluster.SetMetalLBInstallerFactoryForTests(probe))
			t.Cleanup(cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(probe))

			regCtx := newMetalLBDriftContext()
			configure(regCtx.ClusterCfg)

			cmd, _ := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)

			assert.Zero(t, probes, "no load balancer may be probed")

			for _, change := range diff.InPlaceChanges {
				assert.NotEqual(t, specdiff.MetalLBValuesField, change.Field)
				assert.NotEqual(t, specdiff.AWSLoadBalancerControllerValuesField, change.Field)
			}
		})
	}
}

// TestCheckChartValuesDrift_WarnsWhenLoadBalancerCannotBeCompared never turns
// an unreadable release into a clean verdict silently: the update continues,
// but the operator is told the comparison did not happen.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestCheckChartValuesDrift_WarnsWhenLoadBalancerCannotBeCompared(t *testing.T) {
	for name, testCase := range map[string]struct {
		regCtx   *localregistry.Context
		override installerFactoryOverride
	}{
		"metallb": {newMetalLBDriftContext(), cluster.SetMetalLBInstallerFactoryForTests},
		"aws-load-balancer-controller": {
			newAWSLoadBalancerControllerDriftContext(),
			cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(testCase.override(func(*v1alpha1.Cluster) (installer.Installer, error) {
				return &driftingInstaller{err: errValuesProbeFailed}, nil
			}))

			cmd, stderr := newDriftCmd()
			diff := clusterupdate.NewEmptyUpdateResult()
			cluster.ExportCheckChartValuesDrift(cmd, testCase.regCtx, diff)

			assert.Zero(t, diff.TotalChanges())
			assert.Contains(t, stderr.String(), "Cannot compare "+name+" values")
		})
	}
}

// TestMetalLBRendersNoChartValues records why the MetalLB probe reports
// nothing today: KSail passes the chart no values and applies its address pool
// as a resource after install. Rendering values later activates the probe.
func TestMetalLBRendersNoChartValues(t *testing.T) {
	t.Parallel()

	values, err := metallbinstaller.NewInstaller(nil, "", "", 0, "").RenderedValues()

	require.NoError(t, err)
	assert.Empty(t, values)
}

// TestReconcileMetalLBValuesSharesOneUpgrade coalesces a cluster.loadBalancer
// change and MetalLB values drift in one pass into one Helm upgrade.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileMetalLBValuesSharesOneUpgrade(t *testing.T) {
	inst := &driftingInstaller{}

	t.Cleanup(cluster.SetMetalLBInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{
			Field:    "cluster.loadBalancer",
			OldValue: string(v1alpha1.LoadBalancerDefault),
			NewValue: string(v1alpha1.LoadBalancerEnabled),
		},
		{Field: specdiff.MetalLBValuesField},
	}}

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), newMetalLBDriftContext().ClusterCfg, diff, result,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, inst.installs, "both fields share one MetalLB upgrade")
	assert.Len(t, result.AppliedChanges, 2)
}

// TestReconcileLoadBalancerValues_UpgradesOnValuesOnlyDrift applies drift on
// its own: an unchanged ksail.yaml still upgrades the drifted release once.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileLoadBalancerValues_UpgradesOnValuesOnlyDrift(t *testing.T) {
	t.Run("metallb", func(t *testing.T) {
		inst := &driftingInstaller{drifted: true}

		t.Cleanup(cluster.SetMetalLBInstallerFactoryForTests(
			func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
		))

		assertValuesOnlyDriftApplied(t, newMetalLBDriftContext(), specdiff.MetalLBValuesField, "")
		assert.Equal(t, 1, inst.installs)
	})

	t.Run("aws-load-balancer-controller", func(t *testing.T) {
		inst := &driftingEKSLoadBalancerInstaller{drifted: true}

		t.Cleanup(cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
			func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
		))

		assertValuesOnlyDriftApplied(t, newAWSLoadBalancerControllerDriftContext(),
			specdiff.AWSLoadBalancerControllerValuesField, testLoadBalancerRegion)
		assert.Equal(t, 1, inst.installCalls)
		assert.Zero(t, inst.uninstallCalls)
	})
}

// TestReconcileAWSLoadBalancerControllerValuesSharesOneUpgrade coalesces the
// generic load-balancer change, the EKS opt-in and values drift into one Helm
// upgrade, and replays its outcome for every field.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileAWSLoadBalancerControllerValuesSharesOneUpgrade(t *testing.T) {
	inst := &driftingEKSLoadBalancerInstaller{}

	t.Cleanup(cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(
		func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil },
	))

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{
			Field:    "cluster.loadBalancer",
			OldValue: string(v1alpha1.LoadBalancerDisabled),
			NewValue: string(v1alpha1.LoadBalancerEnabled),
		},
		{Field: specdiff.EKSLoadBalancerControllerField, OldValue: "false", NewValue: "true"},
		{Field: specdiff.AWSLoadBalancerControllerValuesField},
	}}

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(),
		newAWSLoadBalancerControllerDriftContext().ClusterCfg,
		diff,
		result,
		testLoadBalancerRegion,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, inst.installCalls, "every field shares one controller upgrade")
	assert.Len(t, result.AppliedChanges, 3)
}

// TestReconcileLoadBalancerValues_IgnoresStaleDrift never installs a load
// balancer the desired configuration no longer asks KSail for.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileLoadBalancerValues_IgnoresStaleDrift(t *testing.T) {
	inst := &driftingInstaller{drifted: true}
	probe := func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil }

	t.Cleanup(cluster.SetMetalLBInstallerFactoryForTests(probe))
	t.Cleanup(cluster.SetAWSLoadBalancerControllerInstallerFactoryForTests(probe))

	cfg := newMetalLBDriftContext().ClusterCfg
	cfg.Spec.Cluster.LoadBalancer = v1alpha1.LoadBalancerDisabled

	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{
		{Field: specdiff.MetalLBValuesField},
		{Field: specdiff.AWSLoadBalancerControllerValuesField},
	}}

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(newReconcileTestCmd(), cfg, diff, result)

	require.NoError(t, err)
	assert.Zero(t, inst.installs)
}

// assertValuesOnlyDriftApplied detects drift through the probe and then
// reconciles exactly that diff, as `cluster update` does.
func assertValuesOnlyDriftApplied(
	t *testing.T,
	regCtx *localregistry.Context,
	field string,
	region string,
) {
	t.Helper()

	cmd, stderr := newDriftCmd()
	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportCheckChartValuesDrift(cmd, regCtx, diff)
	require.Len(t, diff.InPlaceChanges, 1, stderr.String())
	require.Equal(t, field, diff.InPlaceChanges[0].Field)

	regions := []string{}
	if region != "" {
		regions = append(regions, region)
	}

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(
		newReconcileTestCmd(), regCtx.ClusterCfg, diff, result, regions...,
	)

	require.NoError(t, err)
	assert.Len(t, result.AppliedChanges, 1)
}
