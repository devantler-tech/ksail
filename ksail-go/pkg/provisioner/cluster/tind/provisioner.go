package provtind

// TindClusterProvisioner is an implementation of the ClusterProvisioner interface for provisioning talos_in_docker clusters.
type TindClusterProvisioner struct {
}

// Create creates a talos_in_docker cluster.
func (k *TindClusterProvisioner) Create(name string, configPath string) error {
	return nil
}

// Delete deletes a talos_in_docker cluster.
func (k *TindClusterProvisioner) Delete(name string) error {
	return nil
}

// Starts a talos_in_docker cluster.
func (k *TindClusterProvisioner) Start(name string) error {
	return nil
}

// Stops a talos_in_docker cluster.
func (k *TindClusterProvisioner) Stop(name string) error {
	return nil
}

// Lists all talos_in_docker clusters.
func (k *TindClusterProvisioner) List() ([]string, error) {
	return nil, nil
}

// Checks if a talos_in_docker cluster exists.
func (k *TindClusterProvisioner) Exists(name string) (bool, error) {
	return false, nil
}

// / NewTindClusterProvisioner creates a new TindClusterProvisioner.
func NewTindClusterProvisioner() *TindClusterProvisioner {
	return &TindClusterProvisioner{}
}
