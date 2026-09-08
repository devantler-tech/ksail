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
)

var errEKSUpgradeWithRecreation = errors.New(
	"apply EKS control-plane upgrades and recreation-required changes in separate updates; " +
		"align eksctl metadata.version before recreating",
)

type eksVersionPlan struct {
	upgrader clusterupdate.Upgrader
	change   clusterupdate.Change
}

// runEKSUpdate reads the complete diff before any AWS mutation. An explicit upgrade
// cannot be combined with recreation from an independently versioned eksctl file.
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

	err = o.validateEKSRecreation(diff)
	if err != nil {
		return err
	}

	plan, err := o.planEKSVersionUpgrade(provisioner)
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

		notify.Successf(
			o.cmd.OutOrStdout(),
			"Kubernetes upgraded to pinned version %s",
			plan.change.NewValue,
		)

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

func (o *updateOrchestrator) validateEKSRecreation(diff *clusterupdate.UpdateResult) error {
	spec := o.ctx.ClusterCfg.Spec.Cluster
	if spec.EKS.ExperimentalControlPlaneUpgrade &&
		strings.TrimSpace(spec.KubernetesVersion) != "" &&
		diff.HasRecreateRequired() {
		return errEKSUpgradeWithRecreation
	}

	return nil
}

func (plan eksVersionPlan) summary(diff *clusterupdate.UpdateResult) clusterupdate.UpdateResult {
	summary := *diff

	summary.InPlaceChanges = slices.Clone(diff.InPlaceChanges)
	if plan.upgrader != nil {
		summary.InPlaceChanges = append(summary.InPlaceChanges, plan.change)
	}

	return summary
}
