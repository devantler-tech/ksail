package provisioner_k3d

// K3dClusterProvisioner is an implementation of the ClusterProvisioner interface for provisioning k3d clusters.
type K3dClusterProvisioner struct {
}

// Create creates a k3d cluster.
func (k *K3dClusterProvisioner) Create(name string, configPath string) error {
	return nil
}

// Delete deletes a k3d cluster.
func (k *K3dClusterProvisioner) Delete(name string) error {
	return nil
}

// Starts a k3d cluster.
func (k *K3dClusterProvisioner) Start(name string) error {
	return nil
}

// Stops a k3d cluster.
func (k *K3dClusterProvisioner) Stop(name string) error {
	return nil
}

// Lists all k3d clusters.
func (k *K3dClusterProvisioner) List() ([]string, error) {
	return nil, nil
}

// Checks if a k3d cluster exists.
func (k *K3dClusterProvisioner) Exists(name string) (bool, error) {
	return false, nil
}

// / NewK3dClusterProvisioner creates a new K3dClusterProvisioner.
func NewK3dClusterProvisioner() *K3dClusterProvisioner {
	return &K3dClusterProvisioner{}
}
