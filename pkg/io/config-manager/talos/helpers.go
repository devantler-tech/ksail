package talos

import (
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

// ResolveClusterName returns the effective cluster name from Talos config or cluster config.
// Priority: talosConfig.Name > clusterCfg.Spec.Cluster.Connection.Context > DefaultClusterName.
// Returns DefaultClusterName ("talos-default") if both configs are nil or have empty names.
func ResolveClusterName(
	clusterCfg *v1alpha1.Cluster,
	talosConfig *Configs,
) string {
	if talosConfig != nil {
		if name := strings.TrimSpace(talosConfig.Name); name != "" {
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
