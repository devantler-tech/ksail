package talosprovisioner

import (
	"context"
	"fmt"
	"strings"

	svcprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// nodeWithRole holds an IP address and its role for role-aware config application.
type nodeWithRole struct {
	IP   string
	Role string // "control-plane" or "worker"
}

// getNodesByRole returns nodes with their roles for the cluster.
func (p *Provisioner) getNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	if p.dockerClient != nil {
		return p.getDockerNodesByRole(ctx, clusterName)
	}

	if p.hetznerOpts != nil {
		return p.getHetznerNodesByRole(ctx, clusterName)
	}

	if p.omniOpts != nil {
		return p.getOmniNodesByRole(ctx, clusterName)
	}

	return nil, fmt.Errorf("%w: no provider configured for node listing", ErrDockerNotAvailable)
}

// getHetznerNodesByRole gets node IPs and roles from Hetzner servers.
func (p *Provisioner) getHetznerNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	if p.infraProvider == nil {
		return nil, nil
	}

	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return nil, err
	}

	listed, err := p.infraProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list Hetzner nodes: %w", err)
	}

	nodes := make([]nodeWithRole, 0, len(listed))

	for _, node := range listed {
		server, serverErr := requireHetznerServer(ctx, hzProvider, node.Name)
		if serverErr != nil {
			return nil, serverErr
		}

		// Fail closed: a node with no reachable address would otherwise be silently
		// dropped from the set used for config reconcile, upgrade, wipe, and version
		// introspection, risking an inconsistent update that reports success.
		ip, addrErr := hetznerNodeTalosAddress(server)
		if addrErr != nil {
			return nil, fmt.Errorf("resolving address for node %s: %w", node.Name, addrErr)
		}

		nodes = append(nodes, nodeWithRole{IP: ip, Role: node.Role})
	}

	return nodes, nil
}

// getDockerNodesByRole gets node IPs and roles from Docker containers.
// Role is inferred from container names: names containing "controlplane" are control-plane nodes,
// all others are workers.
func (p *Provisioner) getDockerNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	if p.dockerClient == nil {
		return nil, clustererr.ErrDockerClientNotConfigured
	}

	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	nodes := make([]nodeWithRole, 0, len(containers))

	for _, ctr := range containers {
		role := RoleWorker

		for _, name := range ctr.Names {
			// Match both "controlplane" (KSail-scaled nodes) and "control-plane"
			// (Talos SDK-created nodes) naming conventions.
			if strings.Contains(name, "controlplane") || strings.Contains(name, "control-plane") {
				role = RoleControlPlane

				break
			}
		}

		for _, network := range ctr.NetworkSettings.Networks {
			if network.IPAddress != "" {
				nodes = append(nodes, nodeWithRole{
					IP:   network.IPAddress,
					Role: role,
				})

				break
			}
		}
	}

	return nodes, nil
}

// countNodeRoles counts control-plane and worker nodes from a list of nodeWithRole.
func countNodeRoles(nodes []nodeWithRole) (int32, int32) {
	var controlPlanes, workers int32

	for _, n := range nodes {
		switch n.Role {
		case RoleControlPlane:
			controlPlanes++
		case RoleWorker:
			workers++
		}
	}

	if controlPlanes == 0 {
		controlPlanes = 1
	}

	return controlPlanes, workers
}

// countServerNodesByRole counts control-plane and worker nodes from a provider
// node listing.
func countServerNodesByRole(nodes []svcprovider.NodeInfo) (int, int) {
	var controlPlanes, workers int

	for _, node := range nodes {
		switch node.Role {
		case RoleControlPlane:
			controlPlanes++
		case RoleWorker:
			workers++
		}
	}

	return controlPlanes, workers
}

// representativeServerType returns a server type for the given role suitable for
// diffing against desiredType. If any node of the role has a type different from
// desiredType, that differing type is returned so the change is still detected when
// a role holds mixed types (e.g. after a partially-completed rolling replacement).
// When every node already matches (or the role has no node with a known type), the
// first observed type is returned ("" when none).
func representativeServerType(
	nodes []svcprovider.NodeInfo,
	role, desiredType string,
) string {
	var firstSeen string

	for _, node := range nodes {
		if node.Role != role || node.ServerType == "" {
			continue
		}

		if firstSeen == "" {
			firstSeen = node.ServerType
		}

		if !strings.EqualFold(node.ServerType, desiredType) {
			return node.ServerType
		}
	}

	return firstSeen
}
