package provisioner_talos_in_docker

// TalosInDockerClusterProvisioner is an implementation of the ClusterProvisioner interface for provisioning talos_in_docker clusters.
type TalosInDockerClusterProvisioner struct {
}

// Create creates a talos_in_docker cluster.
func (k *TalosInDockerClusterProvisioner) Create(name string, configPath string) error {
	return nil
}

// Delete deletes a talos_in_docker cluster.
func (k *TalosInDockerClusterProvisioner) Delete(name string) error {
	return nil
}

// Starts a talos_in_docker cluster.
func (k *TalosInDockerClusterProvisioner) Start(name string) error {
	return nil
}

// Stops a talos_in_docker cluster.
func (k *TalosInDockerClusterProvisioner) Stop(name string) error {
	return nil
}

// Lists all talos_in_docker clusters.
func (k *TalosInDockerClusterProvisioner) List() ([]string, error) {
	return nil, nil
}

// Checks if a talos_in_docker cluster exists.
func (k *TalosInDockerClusterProvisioner) Exists(name string) (bool, error) {
	return false, nil
}

// / NewTalosInDockerClusterProvisioner creates a new TalosInDockerClusterProvisioner.
func NewTalosInDockerClusterProvisioner() *TalosInDockerClusterProvisioner {
	return &TalosInDockerClusterProvisioner{}
}
