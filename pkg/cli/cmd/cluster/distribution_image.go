package cluster

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// reconcileDistributionImage handles changed boot settings at an unchanged OS
// version, using the same rolling-upgrade path as a version change.
func (o *updateOrchestrator) reconcileDistributionImage(
	upgrader clusterupdate.Upgrader, version string,
) (bool, error) {
	planner, ok := upgrader.(clusterupdate.DistributionImagePlanner)
	if !ok {
		return false, nil
	}

	changed, err := planner.DistributionImageChanged(o.cmd.Context(), o.clusterName)
	if err != nil {
		return false, fmt.Errorf("checking distribution image: %w", err)
	}

	if !changed {
		return false, nil
	}

	if o.dryRun {
		notify.Infof(progressWriter(o.cmd), "Would reconcile distribution image at %s.", version)

		return false, nil
	}

	err = upgrader.UpgradeDistribution(
		o.cmd.Context(),
		o.clusterName,
		version,
		version,
	)
	if err != nil {
		return false, fmt.Errorf("reconciling distribution image at %s: %w", version, err)
	}

	notify.Successf(progressWriter(o.cmd), "Distribution image reconciled at %s.", version)

	return false, nil
}
