package kind

import (
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// DefaultClusterName is the default cluster name for Kind clusters.
const DefaultClusterName = "kind"

// DefaultNetworkName is the Docker network name used by Kind clusters.
const DefaultNetworkName = "kind"

// DefaultMirrorsDir is the default directory name for Kind containerd host mirror configuration.
const DefaultMirrorsDir = "kind/mirrors"

// ResolveMirrorsDir returns the configured mirrors directory or the default.
// It extracts the mirrors directory from the cluster configuration if set,
// otherwise returns DefaultMirrorsDir.
func ResolveMirrorsDir(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg != nil && clusterCfg.Spec.Cluster.Kind.MirrorsDir != "" {
		return clusterCfg.Spec.Cluster.Kind.MirrorsDir
	}

	return DefaultMirrorsDir
}

// ResolveClusterName returns the effective cluster name from Kind config or cluster config.
// Priority: kindConfig.Name > clusterCfg.Spec.Cluster.Connection.Context > "kind" (default).
// Returns "kind" if both configs are nil or have empty names.
func ResolveClusterName(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
) string {
	if kindConfig != nil {
		if name := strings.TrimSpace(kindConfig.Name); name != "" {
			return name
		}
	}

	if clusterCfg != nil {
		if name := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context); name != "" {
			return name
		}
	}

	return DefaultClusterName
}
