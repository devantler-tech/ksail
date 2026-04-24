package hcloudccminstaller

import (
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	hetznerProvider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
)

// ResolveHetznerNetworkName determines the Hetzner Cloud private network name
// for the CCM from the cluster configuration.
//
// Resolution order:
//  1. If spec.provider.hetzner.networkName is explicitly set, use that.
//  2. Otherwise, extract the cluster name from the kubeconfig context
//     (e.g., "admin@dev" → "dev") and append the standard network suffix
//     ("-network") to match what hetzner.Provider.EnsureNetwork creates.
//
// Returns empty string if the network name cannot be determined.
func ResolveHetznerNetworkName(cfg *v1alpha1.Cluster) string {
	// Explicit network name override takes precedence.
	if nn := cfg.Spec.Provider.Hetzner.NetworkName; nn != "" {
		return nn
	}

	// Derive from context: for Talos the context is "admin@<clusterName>".
	clusterName := extractClusterNameFromTalosContext(
		cfg.Spec.Cluster.Connection.Context,
	)
	if clusterName == "" {
		return ""
	}

	return clusterName + hetznerProvider.NetworkSuffix
}

// extractClusterNameFromTalosContext extracts the cluster name from a Talos
// kubeconfig context string. Talos contexts follow the pattern "admin@<name>".
func extractClusterNameFromTalosContext(context string) string {
	const talosPrefix = "admin@"

	name, found := strings.CutPrefix(context, talosPrefix)
	if !found || name == "" {
		return ""
	}

	return name
}
