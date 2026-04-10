package provider

import (
	"context"
	"fmt"
)

// NodeInfo contains information about a node managed by a provider.
type NodeInfo struct {
	// Name is the unique identifier of the node (container name, VM ID, etc.)
	Name string

	// ClusterName is the name of the cluster this node belongs to.
	ClusterName string

	// Role is the role of the node (control-plane, worker).
	Role string

	// State is the current state of the node (running, stopped, etc.)
	State string
}

// ClusterStatus contains the status of a cluster as reported by its infrastructure provider.
// Providers populate Phase and Ready. NodesTotal, NodesReady, and Nodes are best-effort
// and may be zero/empty even on success (e.g., when node listing fails independently).
// Provider-specific fields (e.g., Endpoint) are populated only by providers that support them.
type ClusterStatus struct {
	// Phase is the high-level lifecycle phase (e.g., "running", "RUNNING", "initializing").
	Phase string

	// Ready indicates whether the cluster is considered healthy by the provider.
	Ready bool

	// NodesTotal is the total number of nodes (machines, servers, containers) in the cluster.
	NodesTotal int

	// NodesReady is the number of nodes that are in a healthy/ready state.
	NodesReady int

	// Nodes lists individual node details.
	Nodes []NodeInfo

	// Endpoint is the provider API endpoint URL (populated by cloud providers like Omni).
	Endpoint string
}

// Provider defines the interface for infrastructure providers.
// Providers handle node-level operations independent of the Kubernetes distribution.
type Provider interface {
	// StartNodes starts the nodes for a cluster.
	// If no nodes exist, returns ErrNoNodes.
	StartNodes(ctx context.Context, clusterName string) error

	// StopNodes stops the nodes for a cluster.
	// If no nodes exist, returns ErrNoNodes.
	StopNodes(ctx context.Context, clusterName string) error

	// ListNodes returns all nodes for a specific cluster.
	ListNodes(ctx context.Context, clusterName string) ([]NodeInfo, error)

	// ListAllClusters returns the names of all clusters managed by this provider.
	ListAllClusters(ctx context.Context) ([]string, error)

	// NodesExist returns true if nodes exist for the given cluster name.
	NodesExist(ctx context.Context, clusterName string) (bool, error)

	// DeleteNodes removes all nodes for a cluster.
	// Note: Most provisioners handle node deletion through their SDK,
	// so this is primarily used for cleanup scenarios.
	DeleteNodes(ctx context.Context, clusterName string) error

	// GetClusterStatus returns the provider-level status of a cluster.
	// Returns nil and no error if the cluster does not exist in the provider.
	GetClusterStatus(ctx context.Context, clusterName string) (*ClusterStatus, error)
}

// NodeLister can list nodes for a cluster. This minimal interface is used by
// shared helpers to avoid coupling to the full Provider interface.
type NodeLister interface {
	ListNodes(ctx context.Context, clusterName string) ([]NodeInfo, error)
}

// AvailableProvider is a provider that can report whether it's available.
type AvailableProvider interface {
	NodeLister
	// IsAvailable returns true if the provider is ready for use.
	IsAvailable() bool
}

// EnsureAvailableAndListNodes validates provider availability and returns node list.
// This is a shared helper for provider implementations.
func EnsureAvailableAndListNodes(
	ctx context.Context,
	prov AvailableProvider,
	clusterName string,
) ([]NodeInfo, error) {
	if !prov.IsAvailable() {
		return nil, ErrProviderUnavailable
	}

	nodes, err := prov.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	return nodes, nil
}

// CheckNodesExist returns true if the given cluster has at least one node.
// This is a shared helper for provider implementations that delegate
// NodesExist to ListNodes.
func CheckNodesExist(ctx context.Context, lister NodeLister, clusterName string) (bool, error) {
	nodes, err := lister.ListNodes(ctx, clusterName)
	if err != nil {
		return false, fmt.Errorf("check nodes exist: %w", err)
	}

	return len(nodes) > 0, nil
}

// BuildClusterStatus derives a ClusterStatus from a list of nodes by counting
// how many are in the given readyState. Returns nil if nodes is empty.
func BuildClusterStatus(nodes []NodeInfo, readyState string) *ClusterStatus {
	if len(nodes) == 0 {
		return nil
	}

	nodesReady := 0

	for _, n := range nodes {
		if n.State == readyState {
			nodesReady++
		}
	}

	phase := readyState
	if nodesReady == 0 {
		phase = "stopped"
	} else if nodesReady < len(nodes) {
		phase = "degraded"
	}

	return &ClusterStatus{
		Phase:      phase,
		Ready:      nodesReady == len(nodes),
		NodesTotal: len(nodes),
		NodesReady: nodesReady,
		Nodes:      nodes,
	}
}
