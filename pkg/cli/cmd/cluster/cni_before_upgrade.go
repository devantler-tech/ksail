package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

var (
	errCNINotReconciledBeforeUpgrade = errors.New(
		"the CNI could not be updated before the distribution upgrade, so no node was upgraded",
	)
	errCNIUnverifiedBeforeUpgrade = errors.New(
		"the installed CNI could not be compared with this KSail version before the distribution " +
			"upgrade, so no node was upgraded; resolve the warning above and run update again",
	)
)

// cniRolloutWaiter is implemented by CNI installers whose Helm upgrade returns
// before the CNI runs the new settings. Calico's operator rolls calico-node out
// asynchronously; Cilium's upgrade already waits for its DaemonSet.
type cniRolloutWaiter interface {
	WaitForRollout(ctx context.Context) error
}

// cniDriftProbes are the chart-values probes that compare the installed CNI.
func cniDriftProbes() []chartValuesDriftProbe {
	return []chartValuesDriftProbe{calicoPrerequisitesProbe(), ciliumValuesProbe()}
}

// reconcileCNIBeforeDistributionUpgrade brings the CNI to the settings this KSail
// version renders before the first in-place distribution upgrade step, and waits
// until the CNI runs them (ksail#8281).
//
// A node rebooting into a new distribution release starts its CNI from the pod
// spec already on the cluster. Talos v1.14 removed the FlexVolume host path, so
// an upgrade from v1.13 with the old calico-node spec leaves the node NotReady
// and fails the upgrade before the regular diff would have reapplied Calico.
// Dry runs, recreating upgrades and externally managed upgrades change no running
// node, so they skip this. It runs at most once per update; the later diff then
// finds no CNI drift, so nothing is applied twice.
func (o *updateOrchestrator) reconcileCNIBeforeDistributionUpgrade(
	upgrader clusterupdate.Upgrader,
) error {
	if o.dryRun || o.cniReconciledBeforeUpgrade {
		return nil
	}

	inPlace, ok := upgrader.(clusterupdate.InPlaceDistributionUpgrader)
	if !ok || !inPlace.UpgradesDistributionInPlace() {
		return nil
	}

	o.cniReconciledBeforeUpgrade = true

	diff := clusterupdate.NewEmptyUpdateResult()
	diffEngine := specdiff.NewEngine(
		o.ctx.ClusterCfg.Spec.Cluster.Distribution,
		o.ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	for _, probe := range cniDriftProbes() {
		checkComponentValuesDrift(o.cmd, o.ctx, diffEngine, diff, probe)
	}

	// An unverified CNI may be one the new release cannot start, so stop
	// before any node reboots rather than upgrade on an unknown baseline.
	if diff.HasUnknownBaseline() {
		return errCNIUnverifiedBeforeUpgrade
	}

	if len(diff.InPlaceChanges) == 0 {
		return nil
	}

	return o.applyCNIBeforeUpgrade(diff)
}

// applyCNIBeforeUpgrade reconciles the drifted CNI and waits for its rollout.
// A failure stops the update before any node is upgraded: rebooting nodes into
// a release their CNI cannot start on is what this step exists to prevent.
func (o *updateOrchestrator) applyCNIBeforeUpgrade(diff *clusterupdate.UpdateResult) error {
	ctx := o.cmd.Context()

	for _, change := range diff.InPlaceChanges {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "updating " + change.Field + " before the distribution upgrade",
			Writer:  progressWriter(o.cmd),
		})
	}

	reconciler := newComponentReconciler(o.cmd, o.ctx.ClusterCfg, o.clusterName)
	result := clusterupdate.NewEmptyUpdateResult()

	err := reconciler.reconcileComponents(ctx, diff, result)
	if err != nil {
		return fmt.Errorf("%w: %w", errCNINotReconciledBeforeUpgrade, err)
	}

	err = waitForCNIRollout(ctx, reconciler)
	if err != nil {
		return fmt.Errorf("%w: %w", errCNINotReconciledBeforeUpgrade, err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "CNI updated before the distribution upgrade",
		Writer:  progressWriter(o.cmd),
	})

	return nil
}

// waitForCNIRollout waits for the configured CNI to run its new settings when
// its installer can report that.
func waitForCNIRollout(ctx context.Context, reconciler *componentReconciler) error {
	if !ksailInstallsCalico(reconciler.clusterCfg) || reconciler.factories.Calico == nil {
		return nil
	}

	inst, err := reconciler.factories.Calico(reconciler.clusterCfg)
	if err != nil {
		return fmt.Errorf("create Calico installer: %w", err)
	}

	waiter, ok := inst.(cniRolloutWaiter)
	if !ok {
		return nil
	}

	err = waiter.WaitForRollout(ctx)
	if err != nil {
		return fmt.Errorf("wait for Calico rollout: %w", err)
	}

	return nil
}
