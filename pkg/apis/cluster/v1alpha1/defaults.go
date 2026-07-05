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
	// DefaultKWOKDistributionConfig is the default KWOK distribution configuration directory.
	// KWOK's config loader auto-detects directories and runs kustomize to assemble configs.
	DefaultKWOKDistributionConfig = "kwok"
	// DefaultEKSDistributionConfig is the default EKS distribution configuration filename
	// (declarative eksctl ClusterConfig consumed by the eksctl CLI).
	DefaultEKSDistributionConfig = "eks.yaml"
	// DefaultGKEDistributionConfig is the default GKE distribution configuration filename
	// (declarative containerpb.Cluster spec submitted to the GKE API on create).
	DefaultGKEDistributionConfig = "gke.yaml"
	// DefaultAKSDistributionConfig is the default AKS distribution configuration filename
	// (declarative armcontainerservice.ManagedCluster spec submitted to the AKS API on create).
	DefaultAKSDistributionConfig = "aks.yaml"
	// DefaultSourceDirectory is the default directory for Kubernetes manifests.
	DefaultSourceDirectory = "k8s"
	// DefaultKubeconfigPath is the default path to the kubeconfig file
	// (matches `default:"~/.kube/config"` on Connection.Kubeconfig and
	// OptionsKubernetes.Kubeconfig struct tags).
	DefaultKubeconfigPath = "~/.kube/config"
	// DefaultLocalRegistryPort is the default port for the local registry.
	DefaultLocalRegistryPort int32 = 5050
)

// OIDC default values — canonical source for OIDCSpec struct tag defaults.
// FieldSelector defaults reference these consts instead of restating the
// literals; the table-driven test in defaults_test.go asserts each const
// equals its `default:` struct tag so the two stay in sync.
const (
	// DefaultOIDCUsernameClaim is the default JWT claim mapped to the
	// Kubernetes username (matches `default:"email"` on OIDCSpec.UsernameClaim).
	DefaultOIDCUsernameClaim = "email"
	// DefaultOIDCGroupsClaim is the default JWT claim mapped to Kubernetes
	// group membership (matches `default:"groups"` on OIDCSpec.GroupsClaim).
	DefaultOIDCGroupsClaim = "groups"
	// DefaultOIDCUsernamePrefix is the default prefix prepended to OIDC
	// usernames (matches `default:"oidc:"` on OIDCSpec.UsernamePrefix).
	DefaultOIDCUsernamePrefix = "oidc:"
	// DefaultOIDCGroupsPrefix is the default prefix prepended to OIDC groups
	// (matches `default:"oidc:"` on OIDCSpec.GroupsPrefix).
	DefaultOIDCGroupsPrefix = "oidc:"
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
	// DefaultHetznerServerLimit is the default maximum number of Hetzner servers
	// allowed for a cluster (matches `default:"10"` struct tag on ServerLimit).
	DefaultHetznerServerLimit int32 = 10
)

// DefaultHetznerFallbackLocations returns the default fallback datacenter
// locations tried when server creation fails in the primary location due to
// resource unavailability. All entries are in the eu-central network zone —
// matching the hardcoded network subnet zone (see the Hetzner provider's
// EnsureNetwork) and the default fsn1 primary location — so fallback servers can
// still join the cluster's private network. Slice defaults cannot be expressed
// via a `default:` struct tag, so this is the canonical source applied in code.
// A fresh slice is returned on each call so callers may safely retain or mutate
// the result without affecting the package-level default.
func DefaultHetznerFallbackLocations() []string {
	return []string{"nbg1", "hel1"}
}

// Talos default values — canonical source for OptionsTalos struct tag defaults.
const (
	// DefaultTalosISO is the default Hetzner ISO/image ID for booting Talos
	// Linux 1.12.4 x86 (matches `default:"125127"` struct tag). The prior default
	// (122630, Talos 1.11.2) was deprecated and removed from Hetzner on 2026-03-18.
	// For ARM-based servers, set OptionsTalos.ISO to the matching ARM ISO ID.
	// Keep this in sync with the version-contract default in
	// pkg/fsutil/configmanager/talos (currently TalosVersion1_12).
	DefaultTalosISO int64 = 125127
)

// ExpectedDistributionConfigName returns the default config filename for a distribution.
// Unknown distributions fall back to the Vanilla config filename.
func ExpectedDistributionConfigName(distribution Distribution) string {
	meta, found := distributionMetaFor(distribution)
	if !found {
		return DefaultVanillaDistributionConfig
	}

	return meta.configFile
}

// ExpectedContextName returns the default kube context name for a distribution:
// the distribution's context naming convention applied to its default cluster
// name (e.g. "kind-kind", "k3d-k3d-default", "admin@talos-default"). For EKS the
// IAM identity segment is only known after AWS credentials resolve at create
// time, so scaffolding falls back to the region-less "eks-default.eksctl.io"
// suffix to keep ksail.yaml deterministic. Returns "" for unknown distributions.
func ExpectedContextName(distribution Distribution) string {
	return distribution.ContextName(distribution.DefaultClusterName())
}
