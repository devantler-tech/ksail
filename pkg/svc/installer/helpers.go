package installer

import (
	"time"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
)

const (
	// DefaultInstallTimeout is the default timeout for component installation.
	DefaultInstallTimeout = 5 * time.Minute
)

// GetInstallTimeout determines the timeout for component installation.
// Uses cluster connection timeout if configured, otherwise defaults to DefaultInstallTimeout.
// Returns DefaultInstallTimeout if clusterCfg is nil.
func GetInstallTimeout(clusterCfg *v1alpha1.Cluster) time.Duration {
	if clusterCfg == nil || clusterCfg.Spec.Connection.Timeout.Duration <= 0 {
		return DefaultInstallTimeout
	}

	return clusterCfg.Spec.Connection.Timeout.Duration
}
