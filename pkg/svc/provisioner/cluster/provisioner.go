package clusterprovisioner

import "context"

// ClusterProvisioner defines methods for managing Kubernetes clusters.
type ClusterProvisioner interface {
	// Create creates a Kubernetes cluster. If name is non-empty, target that name; otherwise use config defaults.
	Create(ctx context.Context, name string) error

	// Delete deletes a Kubernetes cluster by name or config default when name is empty.
	Delete(ctx context.Context, name string) error

	// Start starts a Kubernetes cluster by name or config default when name is empty.
	Start(ctx context.Context, name string) error

	// Stop stops a Kubernetes cluster by name or config default when name is empty.
	Stop(ctx context.Context, name string) error

	// List lists all Kubernetes clusters.
	List(ctx context.Context) ([]string, error)

	// Exists checks if a Kubernetes cluster exists by name or config default when name is empty.
	Exists(ctx context.Context, name string) (bool, error)
}
