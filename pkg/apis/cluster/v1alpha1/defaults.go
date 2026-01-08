package v1alpha1

const (
	// DefaultDistributionConfig is the default cluster distribution configuration filename.
	DefaultDistributionConfig = "kind.yaml"
	// DefaultK3dDistributionConfig is the default K3d cluster distribution configuration filename.
	DefaultK3dDistributionConfig = "k3d.yaml"
	// DefaultTalosDistributionConfig is the default Talos cluster distribution configuration directory.
	DefaultTalosDistributionConfig = "talos"
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
	case DistributionKind:
		return DefaultDistributionConfig
	case DistributionK3d:
		return DefaultK3dDistributionConfig
	case DistributionTalos:
		return DefaultTalosDistributionConfig
	default:
		return DefaultDistributionConfig
	}
}

// ExpectedContextName returns the default kube context name for a distribution.
func ExpectedContextName(distribution Distribution) string {
	switch distribution {
	case DistributionKind:
		return "kind-kind"
	case DistributionK3d:
		return "k3d-k3d-default"
	case DistributionTalos:
		return "admin@talos-default"
	default:
		return ""
	}
}
