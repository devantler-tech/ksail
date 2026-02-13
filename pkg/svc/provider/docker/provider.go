package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// LabelScheme defines how to identify and filter containers for a distribution.
type LabelScheme string

const (
	// LabelSchemeKind uses container name prefix "kind-<cluster>-" to identify nodes.
	LabelSchemeKind LabelScheme = "kind"
	// LabelSchemeK3d uses "k3d.cluster" label to identify nodes.
	LabelSchemeK3d LabelScheme = "k3d"
	// LabelSchemeTalos uses "talos.cluster.name" label to identify nodes.
	LabelSchemeTalos LabelScheme = "talos"
	// LabelSchemeVCluster uses container name prefix "vcluster.cp.<cluster>" to identify nodes.
	LabelSchemeVCluster LabelScheme = "vcluster"
)

// Talos label constants.
const (
	LabelTalosOwned       = "talos.owned"
	LabelTalosClusterName = "talos.cluster.name"
	LabelTalosType        = "talos.type"
)

// K3d label constants.
const (
	LabelK3dCluster = "k3d.cluster"
	LabelK3dRole    = "k3d.role"
)

// Default timeouts for Docker operations.
const (
	DefaultStartTimeout = 30 * time.Second
	DefaultStopTimeout  = 60 * time.Second
)

// Provider implements provider.Provider for Docker-based clusters.
type Provider struct {
	client      client.APIClient
	labelScheme LabelScheme
}

// NewProvider creates a new Docker provider with the specified label scheme.
func NewProvider(cli client.APIClient, scheme LabelScheme) *Provider {
	return &Provider{
		client:      cli,
		labelScheme: scheme,
	}
}

// StartNodes starts all containers for the given cluster.
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	return p.forEachNode(ctx, clusterName, DefaultStartTimeout, p.startContainer)
}

// StopNodes stops all containers for the given cluster.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	return p.forEachNode(ctx, clusterName, DefaultStopTimeout, p.stopContainer)
}

// ListNodes returns all nodes for the given cluster based on the label scheme.
func (p *Provider) ListNodes(ctx context.Context, clusterName string) ([]provider.NodeInfo, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	containers, err := p.listContainers(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	nodes := make([]provider.NodeInfo, 0, len(containers))

	for _, c := range containers {
		node := p.containerToNodeInfo(c, clusterName)
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// ListAllClusters returns the names of all clusters managed by this provider.
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	// List all containers and extract unique cluster names
	containers, err := p.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	clusterSet := make(map[string]struct{})

	for _, c := range containers {
		clusterName := p.extractClusterName(c)
		if clusterName != "" {
			clusterSet[clusterName] = struct{}{}
		}
	}

	clusters := make([]string, 0, len(clusterSet))
	for name := range clusterSet {
		clusters = append(clusters, name)
	}

	return clusters, nil
}

// NodesExist returns true if any nodes exist for the given cluster.
func (p *Provider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	if p.client == nil {
		return false, provider.ErrProviderUnavailable
	}

	containers, err := p.listContainers(ctx, clusterName)
	if err != nil {
		return false, err
	}

	return len(containers) > 0, nil
}

// DeleteNodes removes all containers for the given cluster.
// This is a cleanup operation - most provisioners handle deletion through their SDK.
func (p *Provider) DeleteNodes(ctx context.Context, clusterName string) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	containers, err := p.listContainers(ctx, clusterName)
	if err != nil {
		return err
	}

	for _, ctr := range containers {
		// Force remove containers
		err := p.client.ContainerRemove(ctx, ctr.ID, container.RemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		})
		if err != nil {
			return fmt.Errorf("failed to remove container %s: %w", ctr.ID, err)
		}
	}

	return nil
}

// nodeOperation defines a function that operates on a single container.
type nodeOperation func(ctx context.Context, containerName string) error

// IsAvailable returns true if the provider is ready for use.
func (p *Provider) IsAvailable() bool {
	return p.client != nil
}

// forEachNode executes an operation on each node in the cluster.
// It handles common setup: client validation, node listing, empty check, and timeout.
func (p *Provider) forEachNode(
	ctx context.Context,
	clusterName string,
	timeout time.Duration,
	operation nodeOperation,
) error {
	nodes, err := provider.EnsureAvailableAndListNodes(ctx, p, clusterName)
	if err != nil {
		return fmt.Errorf("failed to prepare node operation: %w", err)
	}

	if len(nodes) == 0 {
		return fmt.Errorf("%w: %s", provider.ErrNoNodes, clusterName)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for _, node := range nodes {
		err := operation(timeoutCtx, node.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

// startContainer starts a single container by name.
func (p *Provider) startContainer(ctx context.Context, name string) error {
	err := p.client.ContainerStart(ctx, name, container.StartOptions{})
	if err != nil {
		return fmt.Errorf("failed to start container %s: %w", name, err)
	}

	return nil
}

// stopContainer stops a single container by name.
func (p *Provider) stopContainer(ctx context.Context, name string) error {
	err := p.client.ContainerStop(ctx, name, container.StopOptions{})
	if err != nil {
		return fmt.Errorf("failed to stop container %s: %w", name, err)
	}

	return nil
}

// listContainers returns containers for the given cluster based on the label scheme.
func (p *Provider) listContainers(
	ctx context.Context,
	clusterName string,
) ([]container.Summary, error) {
	switch p.labelScheme {
	case LabelSchemeKind:
		return p.listKindContainers(ctx, clusterName)
	case LabelSchemeK3d:
		return p.listK3dContainers(ctx, clusterName)
	case LabelSchemeTalos:
		return p.listTalosContainers(ctx, clusterName)
	case LabelSchemeVCluster:
		return p.listVClusterContainers(ctx, clusterName)
	default:
		return nil, fmt.Errorf("%w: %s", provider.ErrUnknownLabelScheme, p.labelScheme)
	}
}

// listKindContainers lists containers by name prefix (Kind doesn't use labels).
func (p *Provider) listKindContainers(
	ctx context.Context,
	clusterName string,
) ([]container.Summary, error) {
	// Kind uses container names with format: <cluster>-control-plane, <cluster>-worker, etc.
	containers, err := p.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	prefix := clusterName + "-"

	var result []container.Summary

	for _, ctr := range containers {
		for _, name := range ctr.Names {
			// Container names have leading "/"
			name = strings.TrimPrefix(name, "/")
			if strings.HasPrefix(name, prefix) {
				result = append(result, ctr)

				break
			}
		}
	}

	return result, nil
}

// listContainersByLabels lists containers matching the given label filters.
func (p *Provider) listContainersByLabels(
	ctx context.Context,
	labelFilters ...filters.KeyValuePair,
) ([]container.Summary, error) {
	containers, err := p.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(labelFilters...),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	return containers, nil
}

// listK3dContainers lists containers by K3d labels.
func (p *Provider) listK3dContainers(
	ctx context.Context,
	clusterName string,
) ([]container.Summary, error) {
	return p.listContainersByLabels(ctx,
		filters.Arg("label", LabelK3dCluster+"="+clusterName),
	)
}

// listTalosContainers lists containers by Talos labels.
func (p *Provider) listTalosContainers(
	ctx context.Context,
	clusterName string,
) ([]container.Summary, error) {
	return p.listContainersByLabels(ctx,
		filters.Arg("label", LabelTalosOwned+"=true"),
		filters.Arg("label", LabelTalosClusterName+"="+clusterName),
	)
}

// containerToNodeInfo converts a Docker container to a NodeInfo.
func (p *Provider) containerToNodeInfo(
	ctr container.Summary,
	clusterName string,
) provider.NodeInfo {
	name := ""
	if len(ctr.Names) > 0 {
		name = strings.TrimPrefix(ctr.Names[0], "/")
	}

	role := p.extractRole(ctr)

	return provider.NodeInfo{
		Name:        name,
		ClusterName: clusterName,
		Role:        role,
		State:       ctr.State,
	}
}

// extractClusterName extracts the cluster name from a container based on the label scheme.
func (p *Provider) extractClusterName(ctr container.Summary) string {
	switch p.labelScheme {
	case LabelSchemeKind:
		// Extract from container name: <cluster>-control-plane or <cluster>-worker[N]
		if len(ctr.Names) > 0 {
			name := strings.TrimPrefix(ctr.Names[0], "/")
			// Look for Kind-style suffixes
			for _, suffix := range []string{"-control-plane", "-worker"} {
				if idx := strings.Index(name, suffix); idx > 0 {
					return name[:idx]
				}
			}
		}

		return ""
	case LabelSchemeK3d:
		return ctr.Labels[LabelK3dCluster]
	case LabelSchemeTalos:
		return ctr.Labels[LabelTalosClusterName]
	case LabelSchemeVCluster:
		return extractVClusterName(ctr)
	default:
		return ""
	}
}

// extractRole extracts the node role from a container based on the label scheme.
func (p *Provider) extractRole(ctr container.Summary) string {
	switch p.labelScheme {
	case LabelSchemeKind:
		if len(ctr.Names) > 0 {
			name := strings.TrimPrefix(ctr.Names[0], "/")
			if strings.Contains(name, "control-plane") {
				return "control-plane"
			}

			if strings.Contains(name, "worker") {
				return "worker"
			}
		}

		return ""
	case LabelSchemeK3d:
		return ctr.Labels[LabelK3dRole]
	case LabelSchemeTalos:
		return ctr.Labels[LabelTalosType]
	case LabelSchemeVCluster:
		return extractVClusterRole(ctr)
	default:
		return ""
	}
}

// vCluster container name prefixes.
// vCluster Docker driver names containers as:
//   - Control plane: vcluster.cp.<cluster-name>
//   - Worker nodes:  vcluster.node.<cluster-name>.<worker-name>
//   - Load balancers: vcluster.lb.<cluster-name>.<lb-name>
const (
	vclusterCPPrefix = "vcluster.cp."
	vclusterNodePrefix = "vcluster.node."
	vclusterLBPrefix   = "vcluster.lb."
)

// listVClusterContainers lists containers by vCluster name prefix.
func (p *Provider) listVClusterContainers(
	ctx context.Context,
	clusterName string,
) ([]container.Summary, error) {
	containers, err := p.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	cpPrefix := vclusterCPPrefix + clusterName
	nodePrefix := vclusterNodePrefix + clusterName + "."
	lbPrefix := vclusterLBPrefix + clusterName + "."

	var result []container.Summary

	for _, ctr := range containers {
		for _, name := range ctr.Names {
			name = strings.TrimPrefix(name, "/")
			if name == cpPrefix || strings.HasPrefix(name, nodePrefix) || strings.HasPrefix(name, lbPrefix) {
				result = append(result, ctr)

				break
			}
		}
	}

	return result, nil
}

// extractVClusterName extracts the cluster name from a vCluster container name.
func extractVClusterName(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}

	name := strings.TrimPrefix(ctr.Names[0], "/")

	// Control plane: vcluster.cp.<cluster-name>
	if clusterName, ok := strings.CutPrefix(name, vclusterCPPrefix); ok {
		return clusterName
	}

	// Worker: vcluster.node.<cluster-name>.<worker>
	if rest, ok := strings.CutPrefix(name, vclusterNodePrefix); ok {
		if idx := strings.IndexByte(rest, '.'); idx > 0 {
			return rest[:idx]
		}

		return rest
	}

	// Load balancer: vcluster.lb.<cluster-name>.<lb>
	if rest, ok := strings.CutPrefix(name, vclusterLBPrefix); ok {
		if idx := strings.IndexByte(rest, '.'); idx > 0 {
			return rest[:idx]
		}

		return rest
	}

	return ""
}

// extractVClusterRole determines the role of a vCluster container from its name.
func extractVClusterRole(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}

	name := strings.TrimPrefix(ctr.Names[0], "/")

	if strings.HasPrefix(name, vclusterCPPrefix) {
		return "control-plane"
	}

	if strings.HasPrefix(name, vclusterNodePrefix) {
		return "worker"
	}

	if strings.HasPrefix(name, vclusterLBPrefix) {
		return "load-balancer"
	}

	return ""
}
