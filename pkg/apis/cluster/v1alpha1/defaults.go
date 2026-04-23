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
	// DefaultKWOKDistributionConfig is the default KWOK distribution configuration filename.
	DefaultKWOKDistributionConfig = "kwok.yaml"
	// DefaultEKSDistributionConfig is the default EKS distribution configuration filename
	// (declarative eksctl ClusterConfig consumed by the eksctl CLI).
	DefaultEKSDistributionConfig = "eks.yaml"
	// DefaultSourceDirectory is the default directory for Kubernetes manifests.
	DefaultSourceDirectory = "k8s"
	// DefaultKubeconfigPath is the default path to the kubeconfig file.
	DefaultKubeconfigPath = "~/.kube/config"
	// DefaultLocalRegistryPort is the default port for the local registry.
	DefaultLocalRegistryPort int32 = 5050
)

// SOPS default values — canonical source for SOPS struct tag defaults.
const (
	// DefaultSOPSAgeKeyEnvVar is the default environment variable name
	// for the Age private key (matches `default:"SOPS_AGE_KEY"` on SOPS.AgeKeyEnvVar).
	DefaultSOPSAgeKeyEnvVar = "SOPS_AGE_KEY"
)

// Hetzner default values — canonical source for OptionsHetzner struct tag defaults.
const (
	// DefaultHetznerServerType is the default Hetzner server type for both
	// control-plane and worker nodes (matches `default:"cx23"` struct tag).
	DefaultHetznerServerType = "cx23"
	// DefaultHetznerLocation is the default Hetzner datacenter location
	// (matches `default:"fsn1"` struct tag).
	DefaultHetznerLocation = "fsn1"
	// DefaultHetznerNetworkCIDR is the default CIDR block for the Hetzner
	// private network (matches `default:"10.0.0.0/16"` struct tag).
	DefaultHetznerNetworkCIDR = "10.0.0.0/16"
	// DefaultHetznerTokenEnvVar is the default environment variable name
	// for the Hetzner API token (matches `default:"HCLOUD_TOKEN"` struct tag).
	DefaultHetznerTokenEnvVar = "HCLOUD_TOKEN"
)

// Talos default values — canonical source for OptionsTalos struct tag defaults.
const (
	// DefaultTalosISO is the default Hetzner ISO/image ID for booting Talos
	// Linux (matches `default:"122630"` struct tag).
	// For ARM-based servers, set OptionsTalos.ISO to 122629.
	DefaultTalosISO int64 = 122630
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
	case DistributionKWOK:
		return DefaultKWOKDistributionConfig
	case DistributionEKS:
		return DefaultEKSDistributionConfig
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
	case DistributionKWOK:
		return "kwok-kwok-default"
	case DistributionEKS:
		// eksctl generates kubeconfig contexts as <iam-identity>@<name>.<region>.eksctl.io;
		// the identity segment is only known after AWS credentials resolve at create time.
		// Scaffolding falls back to the region-less suffix so ksail.yaml remains deterministic.
		return "eks-default.eksctl.io"
	default:
		return ""
	}
}
