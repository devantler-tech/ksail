package applecontainer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
)

// Labels carried by every container and volume this provider creates. They are the only thing
// that marks a resource as belonging to a cluster.
const (
	// LabelManagedBy marks a resource as created by KSail.
	LabelManagedBy = "ksail.io/managed-by"
	// LabelCluster holds the name of the cluster a resource belongs to.
	LabelCluster = "ksail.io/cluster"
	// LabelRole holds the node role of a container.
	LabelRole = "ksail.io/role"

	// ManagedByValue is the value of LabelManagedBy on resources this provider owns.
	ManagedByValue = "ksail"
)

// Node roles.
const (
	RoleControlPlane = "control-plane"
	RoleWorker       = "worker"
)

// StateRunning is the state the runtime reports for a started container.
const StateRunning = "running"

// availabilityTimeout bounds the service probe behind IsAvailable, which has no context.
const availabilityTimeout = 10 * time.Second

// containerEntry is the subset of `container list --format json` this provider reads.
type containerEntry struct {
	ID            string `json:"id"`
	Configuration struct {
		Labels map[string]string `json:"labels"`
	} `json:"configuration"`
	Status struct {
		State    string `json:"state"`
		Networks []struct {
			IPv4Address string `json:"ipv4Address"`
		} `json:"networks"`
	} `json:"status"`
}

// volumeEntry is the subset of `container volume list --format json` this provider reads.
type volumeEntry struct {
	ID            string `json:"id"`
	Configuration struct {
		Labels map[string]string `json:"labels"`
	} `json:"configuration"`
}

// Provider implements provider.Provider on Apple's container runtime.
type Provider struct {
	runner Runner
}

// NewProvider creates a provider that drives the runtime through the given Runner.
// A nil Runner yields a provider that reports itself unavailable.
func NewProvider(runner Runner) *Provider {
	return &Provider{runner: runner}
}

// NewDefaultProvider creates a provider that runs the `container` CLI found on PATH.
func NewDefaultProvider() *Provider {
	return NewProvider(NewExecRunner(DefaultBinary))
}

// IsAvailable returns true if the CLI is installed and its service answers.
func (p *Provider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), availabilityTimeout)
	defer cancel()

	return p.CheckAvailable(ctx) == nil
}

// CheckAvailable explains why the runtime cannot be used: ErrProviderUnavailable when no Runner is
// configured, ErrCLINotFound when the CLI is missing, ErrServiceNotRunning when it is installed
// but its service does not answer. It returns nil when the runtime is ready.
func (p *Provider) CheckAvailable(ctx context.Context) error {
	if p.runner == nil {
		return provider.ErrProviderUnavailable
	}

	_, err := p.runner.Run(ctx, "system", "status")
	if err == nil {
		return nil
	}

	if errors.Is(err, ErrCLINotFound) {
		return fmt.Errorf("apple container is unavailable: %w", err)
	}

	return fmt.Errorf("%w: %w", ErrServiceNotRunning, err)
}

// StartNodes starts every stopped node container of the cluster.
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	nodes, err := p.requireNodes(ctx, clusterName)
	if err != nil {
		return err
	}

	// The CLI's start subcommand accepts a single container.
	for _, node := range nodes {
		if node.State == StateRunning {
			continue
		}

		_, err := p.runner.Run(ctx, "start", "--", node.Name)
		if err != nil {
			return fmt.Errorf("failed to start node %s: %w", node.Name, err)
		}
	}

	return nil
}

// StopNodes stops every running node container of the cluster.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	nodes, err := p.requireNodes(ctx, clusterName)
	if err != nil {
		return err
	}

	running := make([]string, 0, len(nodes))

	for _, node := range nodes {
		if node.State == StateRunning {
			running = append(running, node.Name)
		}
	}

	if len(running) == 0 {
		return nil
	}

	_, err = p.runner.Run(ctx, append([]string{"stop", "--"}, running...)...)
	if err != nil {
		return fmt.Errorf("failed to stop nodes of cluster %s: %w", clusterName, err)
	}

	return nil
}

// ListNodes returns the node containers of the cluster, sorted by name.
func (p *Provider) ListNodes(ctx context.Context, clusterName string) ([]provider.NodeInfo, error) {
	entries, err := p.listManagedContainers(ctx)
	if err != nil {
		return nil, err
	}

	nodes := make([]provider.NodeInfo, 0, len(entries))

	for _, entry := range entries {
		if entry.Configuration.Labels[LabelCluster] != clusterName {
			continue
		}

		nodes = append(nodes, provider.NodeInfo{
			Name:        entry.ID,
			ClusterName: clusterName,
			Role:        entry.Configuration.Labels[LabelRole],
			State:       entry.Status.State,
		})
	}

	slices.SortFunc(nodes, func(a, b provider.NodeInfo) int {
		return strings.Compare(a.Name, b.Name)
	})

	return nodes, nil
}

// ListAllClusters returns the sorted names of all clusters with at least one node container.
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	entries, err := p.listManagedContainers(ctx)
	if err != nil {
		return nil, err
	}

	clusters := make([]string, 0, len(entries))

	for _, entry := range entries {
		name := entry.Configuration.Labels[LabelCluster]
		if name != "" && !slices.Contains(clusters, name) {
			clusters = append(clusters, name)
		}
	}

	slices.Sort(clusters)

	return clusters, nil
}

// NodesExist returns true if the cluster has at least one node container.
func (p *Provider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	exists, err := provider.CheckNodesExist(ctx, p, clusterName)
	if err != nil {
		return false, fmt.Errorf("apple container: %w", err)
	}

	return exists, nil
}

// DeleteNodes removes the cluster's node containers and then its volumes. Volumes are found by
// label, so volumes left behind by an earlier failed delete are removed too. Deleting a cluster
// that has neither is not an error.
func (p *Provider) DeleteNodes(ctx context.Context, clusterName string) error {
	nodes, err := p.ListNodes(ctx, clusterName)
	if err != nil {
		return err
	}

	if len(nodes) > 0 {
		args := []string{"delete", "--force", "--"}
		for _, node := range nodes {
			args = append(args, node.Name)
		}

		_, err = p.runner.Run(ctx, args...)
		if err != nil {
			return fmt.Errorf("failed to delete nodes of cluster %s: %w", clusterName, err)
		}
	}

	return p.deleteVolumes(ctx, clusterName)
}

// GetClusterStatus derives the cluster phase and readiness from its containers' states.
func (p *Provider) GetClusterStatus(
	ctx context.Context,
	clusterName string,
) (*provider.ClusterStatus, error) {
	status, err := provider.GetClusterStatusFromLister(ctx, p, clusterName, StateRunning)
	if err != nil {
		return nil, fmt.Errorf("apple container cluster status: %w", err)
	}

	return status, nil
}

// NodeAddress returns the IPv4 address the runtime assigned to a running node. The runtime
// assigns a new address on every start, so callers must read it after starting the node.
func (p *Provider) NodeAddress(
	ctx context.Context,
	clusterName string,
	nodeName string,
) (netip.Addr, error) {
	entries, err := p.listManagedContainers(ctx)
	if err != nil {
		return netip.Addr{}, err
	}

	for _, entry := range entries {
		if entry.ID != nodeName || entry.Configuration.Labels[LabelCluster] != clusterName {
			continue
		}

		for _, network := range entry.Status.Networks {
			prefix, err := netip.ParsePrefix(network.IPv4Address)
			if err == nil && prefix.Addr().Is4() {
				return prefix.Addr(), nil
			}
		}

		return netip.Addr{}, fmt.Errorf("%w: %s", ErrNoAddress, nodeName)
	}

	return netip.Addr{}, fmt.Errorf("%w: %s in cluster %s", ErrNodeNotFound, nodeName, clusterName)
}

// requireNodes lists the cluster's nodes and fails with ErrNoNodes when there are none.
func (p *Provider) requireNodes(
	ctx context.Context,
	clusterName string,
) ([]provider.NodeInfo, error) {
	nodes, err := p.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: %s", provider.ErrNoNodes, clusterName)
	}

	return nodes, nil
}

// listManagedContainers returns every container, running or not, that carries this provider's
// ownership label.
func (p *Provider) listManagedContainers(ctx context.Context) ([]containerEntry, error) {
	entries, err := p.listContainers(ctx)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(entries, func(entry containerEntry) bool {
		return entry.Configuration.Labels[LabelManagedBy] != ManagedByValue
	}), nil
}

// listContainers returns every container on the host, running or not, whoever owns it.
func (p *Provider) listContainers(ctx context.Context) ([]containerEntry, error) {
	if p.runner == nil {
		return nil, provider.ErrProviderUnavailable
	}

	output, err := p.runner.Run(ctx, "list", "--all", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var entries []containerEntry

	err = json.Unmarshal(output, &entries)
	if err != nil {
		return nil, fmt.Errorf("%w: container list: %w", ErrInvalidOutput, err)
	}

	return entries, nil
}

// deleteVolumes removes every volume labelled as belonging to the cluster.
func (p *Provider) deleteVolumes(ctx context.Context, clusterName string) error {
	output, err := p.runner.Run(ctx, "volume", "list", "--format", "json")
	if err != nil {
		return fmt.Errorf("failed to list volumes: %w", err)
	}

	var entries []volumeEntry

	err = json.Unmarshal(output, &entries)
	if err != nil {
		return fmt.Errorf("%w: volume list: %w", ErrInvalidOutput, err)
	}

	args := []string{"volume", "delete", "--"}

	for _, entry := range entries {
		labels := entry.Configuration.Labels
		if labels[LabelManagedBy] == ManagedByValue && labels[LabelCluster] == clusterName {
			args = append(args, entry.ID)
		}
	}

	const argsWithoutVolumes = 3
	if len(args) == argsWithoutVolumes {
		return nil
	}

	_, err = p.runner.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to delete volumes of cluster %s: %w", clusterName, err)
	}

	return nil
}
