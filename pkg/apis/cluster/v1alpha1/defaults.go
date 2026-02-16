package v1alpha1

const (
	// DefaultVanillaDistributionConfig is the default Vanilla cluster distribution configuration filename (uses Kind).
	DefaultVanillaDistributionConfig = "kind.yaml"
	// DefaultK3sDistributionConfig is the default K3s cluster distribution configuration filename.
	DefaultK3sDistributionConfig = "k3d.yaml"
	// DefaultTalosDistributionConfig is the default Talos cluster distribution configuration directory.
	DefaultTalosDistributionConfig = "talos"
	// DefaultVClusterDistributionConfig is the default VCluster distribution configuration filename.
	DefaultVClusterDistributionConfig = "vcluster.yaml"
	// DefaultSourceDirectory is the default directory for Kubernetes manifests.
	DefaultSourceDirectory = "k8s"
	// DefaultKubeconfigPath is the default path to the kubeconfig file.
	DefaultKubeconfigPath = "~/.kube/config"
	// DefaultLocalRegistryPort is the default port for the local registry.
	DefaultLocalRegistryPort int32 = 5050
)

// ExpectedDistributionConfigName returns the default config filename for a distribution.
func ExpectedDistributionConfigName(distribution Distribution) string {
	switch distribution {
	case DistributionVanilla:
		return DefaultVanillaDistributionConfig
	case DistributionK3s:
		return DefaultK3sDistributionConfig
	case DistributionTalos:
		return DefaultTalosDistributionConfig
	case DistributionVCluster:
		return DefaultVClusterDistributionConfig
	default:
		return DefaultVanillaDistributionConfig
	}
}

// ExpectedContextName returns the default kube context name for a distribution.
func ExpectedContextName(distribution Distribution) string {
	switch distribution {
	case DistributionVanilla:
		return "kind-kind"
	case DistributionK3s:
		return "k3d-k3d-default"
	case DistributionTalos:
		return "admin@talos-default"
	case DistributionVCluster:
		return "vcluster-docker_vcluster-default"
	default:
		return ""
	}
}
