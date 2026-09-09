package cluster

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/spf13/cobra"
)

var errEKSUpgradeWithRecreation = errors.New(
	"apply EKS control-plane upgrades and recreation-required changes in separate updates; " +
		"align eksctl metadata.version before recreating",
)

type eksVersionPlan struct {
	upgrader clusterupdate.Upgrader
	change   clusterupdate.Change
}

// runEKSUpdate reads the complete diff and resolves the version plan before any
// AWS mutation. An upgrade that is actually planned cannot be combined with
// recreation from an independently versioned eksctl file.
func (o *updateOrchestrator) runEKSUpdate(
	provisioner clusterprovisioner.Provisioner,
	outputTimer timer.Timer,
) error {
	updater, ok := provisioner.(clusterprovisioner.Updater)
	if !ok {
		return o.runWithoutUpdater()
	}

	currentSpec, diff, err := o.computeUpdateDiff(updater)
	if err != nil {
		return err
	}

	plan, err := o.planEKSVersionUpgrade(provisioner)
	if err != nil {
		return err
	}

	err = validateEKSRecreation(plan.upgrader != nil, diff)
	if err != nil {
		return err
	}

	// Keep the version change in the final preview, but do not route it through
	// the node-group/component Updater after the Upgrader has applied it.
	summary := plan.summary(diff)
	displayChangesSummary(o.cmd, &summary)

	if o.dryRun {
		return reportDryRun(o.cmd, &summary)
	}

	if plan.upgrader != nil {
		err = plan.upgrader.UpgradeKubernetes(
			o.cmd.Context(),
			o.clusterName,
			plan.change.OldValue,
			plan.change.NewValue,
		)
		if err != nil {
			return fmt.Errorf("upgrade EKS control plane: %w", err)
		}

		reportEKSUpgraded(o.cmd, plan.change.NewValue)

		if diff.TotalChanges() == 0 {
			return o.repairEKSComponentState()
		}
	}

	return o.applyOrReportChanges(updater, currentSpec, diff, outputTimer)
}

func (o *updateOrchestrator) planEKSVersionUpgrade(
	provisioner clusterprovisioner.Provisioner,
) (eksVersionPlan, error) {
	upgrader, supportsUpgrade := provisioner.(clusterupdate.Upgrader)

	pin := strings.TrimSpace(o.ctx.ClusterCfg.Spec.Cluster.KubernetesVersion)
	if !supportsUpgrade || pin == "" {
		return eksVersionPlan{}, nil
	}

	planner, ok := upgrader.(clusterupdate.KubernetesUpgradePlanner)
	if !ok {
		return eksVersionPlan{}, nil
	}

	current, err := upgrader.GetCurrentVersions(o.cmd.Context(), o.clusterName)
	if err != nil {
		return eksVersionPlan{}, fmt.Errorf("read EKS control-plane version: %w", err)
	}

	target, err := planner.ValidateKubernetesUpgrade(current.KubernetesVersion, pin)
	if err != nil {
		return eksVersionPlan{}, fmt.Errorf("plan EKS control-plane upgrade: %w", err)
	}

	if versionsEqual(current.KubernetesVersion, target) {
		return eksVersionPlan{}, nil
	}

	return eksVersionPlan{
		upgrader: upgrader,
		change: clusterupdate.Change{
			Field: "kubernetes.version", OldValue: current.KubernetesVersion, NewValue: target,
			Category: planner.KubernetesUpgradeCategory(), Reason: "pinned via configuration",
		},
	}, nil
}

// validateEKSRecreation rejects combining a control-plane upgrade with a
// recreation-required change, because recreation would discard the cluster the
// upgrade is being applied to.
//
// It keys on whether an upgrade is actually planned rather than on the feature
// being enabled with a pin set. Once the control plane has reached its pinned
// version there is no upgrade left to sequence, so a recreation-required change
// is admissible with the pin and the flag both still in place.
func validateEKSRecreation(upgradePlanned bool, diff *clusterupdate.UpdateResult) error {
	if upgradePlanned && diff.HasRecreateRequired() {
		return errEKSUpgradeWithRecreation
	}

	return nil
}

// reportEKSUpgraded confirms a completed control-plane upgrade in text mode.
//
// In JSON mode it emits nothing: displayChangesSummary has already written the
// machine-readable document to the same stream, and the upgrade is represented
// there as the kubernetes.version change. Appending human-readable text would
// leave stdout no longer parseable as JSON.
func reportEKSUpgraded(cmd *cobra.Command, version string) {
	if getOutputFormat(cmd) == outputFormatJSON {
		return
	}

	notify.Successf(cmd.OutOrStdout(), "Kubernetes upgraded to pinned version %s", version)
}

func (plan eksVersionPlan) summary(diff *clusterupdate.UpdateResult) clusterupdate.UpdateResult {
	summary := *diff

	summary.InPlaceChanges = slices.Clone(diff.InPlaceChanges)
	if plan.upgrader != nil {
		summary.InPlaceChanges = append(summary.InPlaceChanges, plan.change)
	}

	return summary
}
