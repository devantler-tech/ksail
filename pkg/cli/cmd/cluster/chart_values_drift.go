package cluster

import (
	"context"
	"slices"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
)

// chartValuesDriftChecker is implemented by installers that compare the values
// they render with the installed release's values (every helmutil.Base-backed
// installer). Asserting it on the factory's result keeps the check on the exact
// installer the reconciler would run, so detection and application render the
// same values.
type chartValuesDriftChecker interface {
	ValuesDrifted(ctx context.Context) (bool, error)
}

type installerFactory = func(*v1alpha1.Cluster) (installer.Installer, error)

// chartValuesDriftProbe names one KSail-managed Helm component whose rendered
// chart values `cluster update` compares with the installed release.
type chartValuesDriftProbe struct {
	// component names the release in warnings and in the change reason.
	component string
	// field is the diff key. The component reconciler must route it to the
	// component's install handler and treat it as an in-place component change.
	field string
	// needed reports whether the desired configuration has KSail install the
	// component. Other clusters have no KSail release to compare.
	needed func(*v1alpha1.Cluster) bool
	// factory selects the component's installer factory.
	factory func(*setup.InstallerFactories) installerFactory
}

// autoscalerValuesProbe compares the Cluster Autoscaler's values (ksail#7366).
func autoscalerValuesProbe() chartValuesDriftProbe {
	return chartValuesDriftProbe{
		component: "cluster-autoscaler",
		field:     specdiff.AutoscalerValuesField,
		needed:    setup.NeedsClusterAutoscalerInstall,
		factory: func(factories *setup.InstallerFactories) installerFactory {
			return factories.ClusterAutoscaler
		},
	}
}

// certManagerValuesProbe compares cert-manager's values (ksail#7444).
func certManagerValuesProbe() chartValuesDriftProbe {
	return chartValuesDriftProbe{
		component: "cert-manager",
		field:     specdiff.CertManagerValuesField,
		needed: func(clusterCfg *v1alpha1.Cluster) bool {
			return setup.GetComponentRequirements(clusterCfg).NeedsCertManager
		},
		factory: func(factories *setup.InstallerFactories) installerFactory {
			return factories.CertManager
		},
	}
}

// chartValuesDriftProbes lists the components whose rendered chart values are
// compared with their installed release. The remaining component families are
// tracked on ksail#7366.
func chartValuesDriftProbes() []chartValuesDriftProbe {
	return []chartValuesDriftProbe{
		autoscalerValuesProbe(),
		certManagerValuesProbe(),
		calicoPrerequisitesProbe(),
	}
}

func calicoPrerequisitesProbe() chartValuesDriftProbe {
	return chartValuesDriftProbe{
		component: "calico",
		field:     specdiff.CalicoPrerequisitesField,
		needed: func(cfg *v1alpha1.Cluster) bool {
			return cfg.Spec.Cluster.CNI == v1alpha1.CNICalico &&
				cfg.Spec.Cluster.Distribution != v1alpha1.DistributionKWOK
		},
		factory: func(factories *setup.InstallerFactories) installerFactory { return factories.Calico },
	}
}

// checkChartValuesDrift runs every chart-values drift probe.
//
// This is the only signal a KSail upgrade that changes a component's rendered
// values produces: the structural diff compares cluster specs, so ksail#7145's
// autoscaler CPU limit reached new clusters only, and `cluster update` on an
// existing one reported success while the old values kept running (ksail#7366).
func checkChartValuesDrift(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	diffEngine *specdiff.Engine,
	diff *clusterupdate.UpdateResult,
) {
	for _, probe := range chartValuesDriftProbes() {
		if probe.field == specdiff.CalicoPrerequisitesField &&
			slices.ContainsFunc(diff.InPlaceChanges, func(change clusterupdate.Change) bool {
				return change.Field == specdiff.CNIField
			}) {
			continue
		}

		checkComponentValuesDrift(cmd, ctx, diffEngine, diff, probe)
	}
}

// checkComponentValuesDrift compares one component's installed release values
// with the values this KSail version renders, and appends an in-place change
// when they differ. Errors are logged as warnings and skipped: they should not
// block the rest of the update.
func checkComponentValuesDrift(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	diffEngine *specdiff.Engine,
	diff *clusterupdate.UpdateResult,
	probe chartValuesDriftProbe,
) {
	if !probe.needed(ctx.ClusterCfg) {
		return
	}

	factory := probe.factory(getInstallerFactories())
	if factory == nil {
		return
	}

	// Pin the probe to the context the other drift probes resolve. Without it
	// the Helm client falls back to the kubeconfig's current-context, and the
	// check would read (and schedule an upgrade from) whatever cluster that
	// points at.
	probeCfg := *ctx.ClusterCfg
	probeCfg.Spec.Cluster.Connection.Context = resolveKubeContext(ctx)

	inst, err := factory(&probeCfg)
	if err != nil {
		notify.Warningf(cmd.ErrOrStderr(),
			"Cannot build the %s installer for values drift detection: %v", probe.component, err)

		return
	}

	checker, ok := inst.(chartValuesDriftChecker)
	if !ok {
		return
	}

	drifted, err := checker.ValuesDrifted(cmd.Context())
	if err != nil {
		notify.Warningf(cmd.ErrOrStderr(),
			"Cannot compare %s values for drift detection: %v", probe.component, err)

		return
	}

	diffEngine.CheckChartValues(probe.field, probe.component, drifted, diff)
}
