package talos

import "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"

// WithProviderNetworks applies the Kubernetes provider's authoritative nested
// networks. Other providers retain their distribution configuration unchanged.
func (c *Configs) WithProviderNetworks(cluster *v1alpha1.Cluster) (*Configs, error) {
	if cluster.Spec.Cluster.Provider != v1alpha1.ProviderKubernetes {
		return c, nil
	}

	network := cluster.Spec.Provider.Kubernetes
	podCIDR, serviceCIDR := network.PodCIDR, network.ServiceCIDR

	if podCIDR == "" {
		podCIDR = v1alpha1.DefaultKubernetesPodCIDR
	}

	if serviceCIDR == "" {
		serviceCIDR = v1alpha1.DefaultKubernetesServiceCIDR
	}

	return c.WithKubernetesNetwork(podCIDR, serviceCIDR)
}
