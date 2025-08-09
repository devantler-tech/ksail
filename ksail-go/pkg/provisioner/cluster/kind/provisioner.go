package provisioner_kind

// KindClusterProvisioner is an implementation of the ClusterProvisioner interface for provisioning kind clusters.
type KindClusterProvisioner struct {
}

// Create creates a kind cluster.
func (k *KindClusterProvisioner) Create(name string, configPath string) error {
	return nil
}

// Delete deletes a kind cluster.
func (k *KindClusterProvisioner) Delete(name string) error {
	return nil
}

// Starts a kind cluster.
func (k *KindClusterProvisioner) Start(name string) error {
	return nil
}

// Stops a kind cluster.
func (k *KindClusterProvisioner) Stop(name string) error {
	return nil
}

// Lists all kind clusters.
func (k *KindClusterProvisioner) List() ([]string, error) {
	return nil, nil
}

// Checks if a kind cluster exists.
func (k *KindClusterProvisioner) Exists(name string) (bool, error) {
	return false, nil
}

// / NewKindClusterProvisioner creates a new KindClusterProvisioner.
func NewKindClusterProvisioner() *KindClusterProvisioner {
	return &KindClusterProvisioner{}
}
