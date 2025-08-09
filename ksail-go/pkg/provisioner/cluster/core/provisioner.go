package provcore

// ClusterProvisioner defines methods for managing Kubernetes clusters.
type ClusterProvisioner interface {
	// Create creates a Kubernetes cluster.
	Create() error

	// Delete deletes a Kubernetes cluster.
	Delete() error

	// Start starts a Kubernetes cluster.
	Start() error

	// Stop stops a Kubernetes cluster.
	Stop() error

	// List lists all Kubernetes clusters.
	List() ([]string, error)

	// Exists checks if a Kubernetes cluster exists.
	Exists() (bool, error)
}
