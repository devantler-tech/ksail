package talos

import (
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

// ResolveClusterName returns the effective cluster name from Talos config or cluster config.
// Priority: talosConfig.Name > clusterCfg.Spec.Cluster.Connection.Context > DefaultClusterName.
// When using the context, extracts the cluster name from the "admin@<cluster-name>" pattern.
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
		if ctx := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context); ctx != "" {
			// Extract cluster name from "admin@<cluster-name>" pattern
			if clusterName, ok := strings.CutPrefix(ctx, "admin@"); ok && clusterName != "" {
				return clusterName
			}
			// Fall back to raw context if pattern doesn't match
			return ctx
		}
	}

	return DefaultClusterName
}
