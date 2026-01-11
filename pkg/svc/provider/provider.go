package provider

import "context"

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
}
