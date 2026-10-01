package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Failure diagnostics must not keep an interrupted upgrade waiting indefinitely.
const upgradeFailureReadTimeout = 10 * time.Second

// reportFailedUpgradeStep keeps the attempted path separate from observed state:
// an upgrade can change the cluster before returning an error.
func (o *updateOrchestrator) reportFailedUpgradeStep(
	params versionUpgradeParams,
	step, total int,
	fromVersion, toVersion string,
	applyErr error,
) (bool, error) {
	ctx, cancel := context.WithTimeout(o.cmd.Context(), upgradeFailureReadTimeout)
	defer cancel()

	versions, readErr := params.upgrader.GetCurrentVersions(ctx, o.clusterName)

	observation := observedUpgradeVersions(versions)
	if readErr != nil {
		observation = fmt.Sprintf("running versions could not be determined (%v)", readErr)
	}

	err := fmt.Errorf("%s upgrade failed at step %d/%d (%s → %s), %s: %w",
		params.upgradeType, step, total, fromVersion, toVersion, observation, applyErr)
	notify.Warningf(progressWriter(o.cmd), "%v", err)

	return false, err
}

func observedUpgradeVersions(versions *clusterupdate.VersionInfo) string {
	if versions == nil ||
		(strings.TrimSpace(versions.KubernetesVersion) == "" &&
			strings.TrimSpace(versions.DistributionVersion) == "") {
		return "running versions could not be determined"
	}

	kubernetes := strings.TrimSpace(versions.KubernetesVersion)
	if kubernetes == "" {
		kubernetes = "unknown"
	}

	distribution := strings.TrimSpace(versions.DistributionVersion)
	if distribution == "" {
		distribution = "unknown"
	}

	return fmt.Sprintf(
		"observed versions: Kubernetes %s, distribution %s",
		kubernetes,
		distribution,
	)
}
