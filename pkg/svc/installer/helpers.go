package installer

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

const (
	// DefaultInstallTimeout is the default timeout for component installation.
	DefaultInstallTimeout = 5 * time.Minute
	// TalosInstallTimeout is the default timeout for Talos component installation.
	// Talos clusters take longer to bootstrap due to the immutable OS design,
	// so we use a longer timeout (10 minutes) per operation.
	TalosInstallTimeout = 10 * time.Minute
)

// GetInstallTimeout determines the timeout for component installation.
// Uses cluster connection timeout if configured, otherwise defaults to:
//   - TalosInstallTimeout (10m) for TalosInDocker distribution
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

	// Use longer timeout for TalosInDocker
	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalosInDocker {
		return TalosInstallTimeout
	}

	return DefaultInstallTimeout
}
