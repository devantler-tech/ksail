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
//  1. Extract the cluster name from the kubeconfig context
//     (e.g., "admin@dev" → "dev") and append the standard network suffix
//     ("-network") to match what hetzner.Provider.EnsureNetwork creates.
//  2. If the effective created network name cannot be derived from the
//     context, fall back to spec.provider.hetzner.networkName if set.
//
// Returns empty string if the network name cannot be determined.
func ResolveHetznerNetworkName(cfg *v1alpha1.Cluster) string {
	// Derive the effective network name used by the Hetzner provider.
	// For Talos the context is "admin@<clusterName>".
	clusterName := extractClusterNameFromTalosContext(
		cfg.Spec.Cluster.Connection.Context,
	)
	if clusterName != "" {
		return clusterName + hetznerProvider.NetworkSuffix
	}

	// Fall back to an explicitly configured name only when the effective
	// provider-created network name cannot be derived.
	if nn := cfg.Spec.Provider.Hetzner.NetworkName; nn != "" {
		return nn
	}

	return ""
}

// extractClusterNameFromTalosContext extracts the cluster name from a Talos
// kubeconfig context string. Talos contexts follow the pattern "admin@<name>".
func extractClusterNameFromTalosContext(context string) string {
	const talosPrefix = "admin@"

	context = strings.TrimSpace(context)

	name, found := strings.CutPrefix(context, talosPrefix)
	name = strings.TrimSpace(name)

	if !found || name == "" {
		return ""
	}

	return name
}
