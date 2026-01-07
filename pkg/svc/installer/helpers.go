package installer

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

const (
	// DefaultInstallTimeout is the default timeout (5 minutes) for component installation.
	DefaultInstallTimeout = 5 * time.Minute
	// TalosInstallTimeout is the timeout (5 minutes) for Talos component installation.
	// Talos clusters take longer to bootstrap due to the immutable OS design.
	TalosInstallTimeout = 5 * time.Minute
)

// GetInstallTimeout determines the timeout for component installation.
// Uses cluster connection timeout if configured, otherwise defaults to:
//   - TalosInstallTimeout (5m) for Talos distribution
//   - DefaultInstallTimeout (5m) for all other distributions
//
// Returns DefaultInstallTimeout if clusterCfg is nil.
func GetInstallTimeout(clusterCfg *v1alpha1.Cluster) time.Duration {
	if clusterCfg == nil {
		return DefaultInstallTimeout
	}

	// Use explicit timeout if configured
	if clusterCfg.Spec.Cluster.Connection.Timeout.Duration > 0 {
		return clusterCfg.Spec.Cluster.Connection.Timeout.Duration
	}

	// Use longer timeout for Talos
	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos {
		return TalosInstallTimeout
	}

	return DefaultInstallTimeout
}
