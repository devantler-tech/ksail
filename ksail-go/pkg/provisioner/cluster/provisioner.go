package clusterprovisioner

// ClusterProvisioner defines methods for managing Kubernetes clusters.
type ClusterProvisioner interface {
	// Create creates a Kubernetes cluster. If name is non-empty, target that name; otherwise use config defaults.
	Create(name string) error

	// Delete deletes a Kubernetes cluster by name or config default when name is empty.
	Delete(name string) error

	// Start starts a Kubernetes cluster by name or config default when name is empty.
	Start(name string) error

	// Stop stops a Kubernetes cluster by name or config default when name is empty.
	Stop(name string) error

	// List lists all Kubernetes clusters.
	List() ([]string, error)

	// Exists checks if a Kubernetes cluster exists by name or config default when name is empty.
	Exists(name string) (bool, error)
}
