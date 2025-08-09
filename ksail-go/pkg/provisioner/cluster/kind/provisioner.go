package kindProvisioner

import (
	ksail "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	"sigs.k8s.io/kind/pkg/cluster"
)

// KindClusterProvisioner is an implementation of the ClusterProvisioner interface for provisioning kind clusters.
type KindClusterProvisioner struct {
  ksailConfig *ksail.Cluster
  dockerProvider *cluster.Provider
}

// Create creates a kind cluster.
func (k *KindClusterProvisioner) Create(name string, configPath string) error {
	return k.dockerProvider.Create(name, cluster.CreateWithConfigFile(configPath))
}

// Delete deletes a kind cluster.
func (k *KindClusterProvisioner) Delete(name string) error {
	return k.dockerProvider.Delete(name, k.ksailConfig.Spec.Connection.Kubeconfig)
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
func NewKindClusterProvisioner(ksailConfig *ksail.Cluster) *KindClusterProvisioner {
	return &KindClusterProvisioner{
		ksailConfig:  ksailConfig,
		dockerProvider: cluster.NewProvider(),
  }
}
