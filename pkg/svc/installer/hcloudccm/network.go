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
//     This matches the API contract: "If empty, a network named
//     '<cluster-name>-network' will be created."
//  2. Extract the cluster name from the kubeconfig context
//     (e.g., "admin@dev" → "dev") and append the standard network suffix
//     ("-network") to match what hetzner.Provider.EnsureNetwork creates.
//  3. Use the provided clusterName fallback (from the CLI / provisioner) and
//     append the network suffix. This ensures the installer always matches
//     the network that hetzner.Provider.EnsureNetwork creates, even when
//     Connection.Context is empty or doesn't follow the "admin@<name>" pattern.
//
// Returns empty string if the network name cannot be determined.
func ResolveHetznerNetworkName(cfg *v1alpha1.Cluster, clusterName string) string {
	// Explicit network name takes precedence per the API contract.
	if nn := cfg.Spec.Provider.Hetzner.NetworkName; nn != "" {
		return nn
	}

	// Derive from context: for Talos the context is "admin@<clusterName>".
	contextName := ExtractClusterNameFromTalosContext(
		cfg.Spec.Cluster.Connection.Context,
	)
	if contextName != "" {
		return contextName + hetznerProvider.NetworkSuffix
	}

	// Fallback: use the directly-provided cluster name. This matches the
	// value passed to hetzner.Provider.EnsureNetwork, guaranteeing the
	// installer and provider agree on the network name.
	clusterName = strings.TrimSpace(clusterName)
	if clusterName != "" {
		return clusterName + hetznerProvider.NetworkSuffix
	}

	return ""
}

// ExtractClusterNameFromTalosContext extracts the cluster name from a Talos
// kubeconfig context string. Talos contexts follow the pattern "admin@<name>".
func ExtractClusterNameFromTalosContext(context string) string {
	const talosPrefix = "admin@"

	context = strings.TrimSpace(context)

	name, found := strings.CutPrefix(context, talosPrefix)
	name = strings.TrimSpace(name)

	if !found || name == "" {
		return ""
	}

	return name
}
