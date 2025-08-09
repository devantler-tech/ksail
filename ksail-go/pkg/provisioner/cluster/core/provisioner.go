package provisioner_core

import "context"

// ClusterProvisioner defines methods for managing Kubernetes clusters.
type ClusterProvisioner interface {
	// Create creates a Kubernetes cluster.
	Create(name string, configPath string, ctx context.Context) error

	// Delete deletes a Kubernetes cluster.
	Delete(name string, ctx context.Context) error

	// Start starts a Kubernetes cluster.
	Start(name string, ctx context.Context) error

	// Stop stops a Kubernetes cluster.
	Stop(name string, ctx context.Context) error

	// List lists all Kubernetes clusters.
	List(ctx context.Context) ([]string, error)

	// Exists checks if a Kubernetes cluster exists.
	Exists(name string, ctx context.Context) (bool, error)
}
