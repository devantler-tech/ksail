package image

import (
	"context"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
)

// validateImageOpParams validates the distribution and provider for an image
// export/import operation. It is shared by both Exporter and Importer so the
// unsupported-distribution and Docker-only provider guards stay in lock-step.
//
// Talos and VCluster are not supported: Talos is an immutable OS without shell
// access, and VCluster (Vind) runs its own containerd inside Docker containers
// without standard exec-based image import support. Only the Docker provider is
// supported via the Docker SDK; an empty provider is treated as Docker.
func validateImageOpParams(
	distribution v1alpha1.Distribution,
	providerType v1alpha1.Provider,
) error {
	if distribution == v1alpha1.DistributionTalos ||
		distribution == v1alpha1.DistributionVCluster {
		return ErrUnsupportedDistribution
	}

	if providerType != v1alpha1.ProviderDocker && providerType != "" {
		return fmt.Errorf(
			"%w: %s (only Docker provider supported)",
			ErrUnsupportedProvider,
			providerType,
		)
	}

	return nil
}

// listClusterNodes lists the Docker nodes for a cluster using the distribution's
// label scheme. It returns ErrNoNodes when the cluster has no nodes. Role-based
// filtering (export node selection vs. import node filtering) is left to the
// caller, which differs between Exporter and Importer.
func listClusterNodes(
	ctx context.Context,
	dockerClient dockerclient.Client,
	clusterName string,
	distribution v1alpha1.Distribution,
) ([]provider.NodeInfo, error) {
	labelScheme := getLabelScheme(distribution)

	dockerProvider := dockerprovider.NewProvider(dockerClient, labelScheme)

	nodes, err := dockerProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: cluster %s", ErrNoNodes, clusterName)
	}

	return nodes, nil
}

// getLabelScheme returns the Docker provider label scheme for a distribution.
func getLabelScheme(distribution v1alpha1.Distribution) dockerprovider.LabelScheme {
	if distribution == v1alpha1.DistributionK3s {
		return dockerprovider.LabelSchemeK3d
	}

	return dockerprovider.LabelSchemeKind
}

// Node role priorities for export selection.
const (
	rolePriorityControlPlane    = 0
	rolePriorityServer          = 1
	rolePriorityWorker          = 2
	rolePriorityAgent           = 3
	rolePriorityUnknown         = 4
	rolePriorityUnselectedStart = 999
)

// selectNodeForExport selects a suitable node for export operations.
// It prefers control-plane/server nodes over workers, and filters out
// helper containers (tools, loadbalancer, etc.) that don't have containerd.
func selectNodeForExport(nodes []provider.NodeInfo) string {
	// Define preferred role order - control-plane/server nodes first
	rolePreference := map[string]int{
		"control-plane": rolePriorityControlPlane, // Kind control-plane
		"server":        rolePriorityServer,       // K3d server (control-plane)
		"worker":        rolePriorityWorker,       // Kind/K3d worker nodes
		"agent":         rolePriorityAgent,        // K3d agent nodes
		"":              rolePriorityUnknown,      // Unknown role - fallback
	}

	var bestNode provider.NodeInfo

	bestPriority := rolePriorityUnselectedStart

	for _, node := range nodes {
		// Skip helper containers without containerd
		if isHelperContainer(node.Role) {
			continue
		}

		priority, ok := rolePreference[node.Role]
		if !ok {
			priority = rolePriorityUnknown // Unknown role gets low priority
		}

		if priority < bestPriority {
			bestPriority = priority
			bestNode = node
		}
	}

	return bestNode.Name
}

// Temp path constants for different distributions.
const (
	tmpPathRoot = "/root" // Kind containers
	tmpPathTmp  = "/tmp"  // K3d containers
)

// getTempPath returns the appropriate temp directory path for a distribution.
// Kind containers have tmpfs on /tmp which Docker cp can't access properly,
// so we use /root instead. K3d containers don't have /root but /tmp works fine.
func getTempPath(distribution v1alpha1.Distribution) string {
	if distribution == v1alpha1.DistributionVanilla {
		return tmpPathRoot // Kind has tmpfs on /tmp
	}

	return tmpPathTmp
}
