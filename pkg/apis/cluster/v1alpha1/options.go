package v1alpha1

// --- Distribution-specific Options Types ---

// OptionsVanilla defines options specific to the Vanilla distribution (uses Kind with Docker provider).
// Node counts should be configured directly in kind.yaml.
type OptionsVanilla struct {
	// MirrorsDir is the directory for containerd host mirror configuration.
	// Defaults to "kind/mirrors" if not specified.
	MirrorsDir string `json:"mirrorsDir,omitzero"`
}

// OptionsTalos defines options specific to the Talos distribution.
type OptionsTalos struct {
	// Version pins the Talos OS version used for cluster creation and upgrades.
	// When set, KSail uses this version as the Docker container image tag and
	// caps `--update-distribution` upgrades at this version. Accepts values
	// with or without the "v" prefix (e.g., "v1.11.2" or "1.11.2").
	// When empty, KSail uses its built-in default version.
	Version string `json:"version,omitzero"`
	// ControlPlanes is the number of control-plane nodes (default: 1).
	//
	// Deprecated: use spec.cluster.controlPlanes. This field is kept as a
	// migration alias and emits a warning on load. Removal planned in a
	// future minor release.
	ControlPlanes int32 `json:"controlPlanes,omitzero" jsonschema:"description=DEPRECATED: use spec.cluster.controlPlanes instead,minimum=0"` //nolint:lll
	// Workers is the number of worker nodes (default: 0).
	// When 0, scheduling is allowed on control-plane nodes.
	//
	// Deprecated: use spec.cluster.workers. This field is kept as a
	// migration alias and emits a warning on load. Removal planned in a
	// future minor release.
	Workers int32 `json:"workers,omitzero" jsonschema:"description=DEPRECATED: use spec.cluster.workers instead,minimum=0"` //nolint:lll
	// Config is the path to the talosconfig file.
	// Defaults to "~/.talos/config".
	Config string `default:"~/.talos/config" json:"config,omitzero"`
	// ISO is the cloud provider's ISO/image ID for booting Talos Linux.
	// Only used when targeting cloud providers (e.g., Hetzner Cloud).
	// For Hetzner: See https://docs.hetzner.cloud/changelog for available Talos ISOs.
	// Defaults to 122630 (Talos Linux 1.11.2 x86). Use 122629 for ARM.
	// When SchematicID is set, ISO is ignored in favour of a pre-built snapshot.
	ISO int64 `default:"122630" json:"iso,omitzero"`
	// SchematicID is the Talos factory schematic ID used to build a Hetzner snapshot image.
	// When set, KSail uploads a Talos OS disk snapshot using this schematic ID and Version
	// instead of booting from the cloud ISO specified in ISO.
	// Obtain a schematic ID from https://factory.talos.dev.
	// Only used when targeting cloud providers (e.g., Hetzner Cloud).
	SchematicID string `json:"schematicId,omitzero"`
	// Extensions lists Talos Image Factory official system extension names to include in the
	// node image. KSail automatically computes the Image Factory schematic ID from this list
	// and sets machine.install.image to factory.talos.dev/installer/{schematicID}:{version},
	// where {version} is derived from the Talos config bundle's existing install image tag.
	// For Hetzner, the schematic is also used for snapshot building.
	// Extension names follow the Image Factory convention (e.g., "siderolabs/iscsi-tools").
	// The Image Factory resolves extension versions automatically per Talos release.
	// When SchematicID is also set, it takes precedence over Extensions.
	Extensions []string `json:"extensions,omitzero"`
	// ExtraPortMappings defines additional port mappings from Docker containers to the host.
	// Only used with the Docker provider. Useful on macOS where MetalLB virtual IPs
	// are not accessible from the host because Docker runs in a Linux VM.
	// Ports are exposed on the first control-plane node (when multiple control-planes are configured).
	ExtraPortMappings []PortMapping `json:"extraPortMappings,omitzero"`
	// ImageVerification enables scaffolding of a Talos ImageVerificationConfig document
	// during cluster init. When Enabled, generates an image-verification.yaml template
	// in the Talos patches directory with commented-out examples for keyless (Cosign/OIDC)
	// and public key verification rules. Requires Talos 1.13+.
	ImageVerification ImageVerification `json:"imageVerification,omitzero"`
}

// PortMapping defines a mapping between a container port and a host port.
type PortMapping struct {
	// ContainerPort is the port inside the container.
	ContainerPort int32 `json:"containerPort"`
	// HostPort is the port on the host. If 0, Docker assigns a random port.
	HostPort int32 `json:"hostPort,omitzero"`
	// Protocol is the network protocol (TCP or UDP). Defaults to TCP.
	Protocol string `default:"TCP" json:"protocol,omitzero" jsonschema:"enum=TCP,enum=UDP,default=TCP"`
}

// LocalRegistry defines options for the host-local OCI registry integration.
// For cloud providers (e.g., Hetzner), this can be used to configure an external registry.
type LocalRegistry struct {
	// Registry is the full registry specification in the format: [user:pass@]host[:port][/path]
	// When populated, enables registry integration for GitOps workflows.
	// Examples:
	//   - "localhost:5050" (local Docker registry)
	//   - "ghcr.io/myorg/myrepo" (GitHub Container Registry with path)
	//   - "${USER}:${PASS}@ghcr.io:443/myorg" (with credentials from env vars)
	// Credentials support ${ENV_VAR} placeholders for environment variable expansion.
	Registry string `json:"registry,omitzero"`
}

// OptionsHetzner defines options specific to the Hetzner Cloud provider.
// These options are used when Provider is set to "Hetzner" for the Talos distribution.
type OptionsHetzner struct {
	// ControlPlaneServerType is the Hetzner server type for control-plane nodes.
	// Examples: "cx23" (x86), "cax11" (ARM), "cpx21" (AMD). Defaults to "cx23".
	ControlPlaneServerType string `default:"cx23" json:"controlPlaneServerType,omitzero"`
	// WorkerServerType is the Hetzner server type for worker nodes.
	// Examples: "cx23" (x86), "cax11" (ARM), "cpx21" (AMD). Defaults to "cx23".
	WorkerServerType string `default:"cx23" json:"workerServerType,omitzero"`
	// Location is the Hetzner datacenter location.
	// Examples: "fsn1" (Falkenstein), "nbg1" (Nuremberg), "hel1" (Helsinki).
	// Defaults to "fsn1".
	Location string `default:"fsn1" json:"location,omitzero"`
	// NetworkName is the name of the private network to create or use.
	// If empty, a network named "<cluster-name>-network" will be created.
	NetworkName string `json:"networkName,omitzero"`
	// NetworkCIDR is the CIDR block for the private network.
	// Defaults to "10.0.0.0/16".
	NetworkCIDR string `default:"10.0.0.0/16" json:"networkCidr,omitzero"`
	// SSHKeyName is the name of the SSH key to use for server access.
	// The key must already exist in the Hetzner Cloud project.
	// If empty, no SSH key is attached (only Talos API access).
	SSHKeyName string `json:"sshKeyName,omitzero"`
	// TokenEnvVar is the environment variable containing the Hetzner API token.
	// Defaults to "HCLOUD_TOKEN".
	TokenEnvVar string `default:"HCLOUD_TOKEN" json:"tokenEnvVar,omitzero"`
	// PlacementGroupStrategy controls whether and how placement groups are used.
	// "Spread" (default) distributes servers across different physical hosts for HA.
	// "None" disables placement groups, useful when Hetzner resources are constrained.
	// Note: Spread groups are limited to 10 servers per datacenter.
	PlacementGroupStrategy PlacementGroupStrategy `default:"Spread" json:"placementGroupStrategy,omitzero"`
	// PlacementGroup is the name of the placement group for server distribution.
	// If empty, a placement group named "<cluster-name>-placement" will be created.
	// Only used when PlacementGroupStrategy is "Spread".
	PlacementGroup string `json:"placementGroup,omitzero"`
	// FallbackLocations specifies alternative datacenter locations to try when
	// server creation fails in the primary location due to resource unavailability.
	// Defaults to ["nbg1", "hel1"] (Nuremberg, Helsinki) as fallbacks for fsn1 (Falkenstein).
	// All locations should be in the same network zone (eu-central) for consistency.
	FallbackLocations []string `json:"fallbackLocations,omitzero"`
	// PlacementGroupFallbackToNone allows automatic fallback to no placement group
	// when spread placement constraints cannot be satisfied (e.g., due to datacenter capacity).
	// When true and placement fails, retries server creation without a placement group.
	// Defaults to false to preserve HA guarantees; set to true for best-effort provisioning.
	PlacementGroupFallbackToNone bool `json:"placementGroupFallbackToNone,omitzero"`
	// IngressFirewall controls the Talos OS-level ingress firewall configuration.
	// When Enabled (default), KSail generates NetworkDefaultActionConfig and NetworkRuleConfig
	// documents as Talos machine config patches, providing defense-in-depth at the node level
	// independent of the Hetzner Cloud Firewall.
	// See: https://www.talos.dev/latest/talos-guides/network/ingress-firewall/
	IngressFirewall IngressFirewall `default:"Enabled" json:"ingressFirewall,omitzero"`
	// ServerLimit is the maximum number of Hetzner servers (control-plane + worker + autoscaler
	// pool capacity) permitted in this cluster. Used by ValidateAutoscalerConfig to prevent
	// the configured autoscaler capacity from exceeding the account/project server quota.
	// When set to 0, KSail uses DefaultHetznerServerLimit instead of treating 0 as an explicit
	// limit. Defaults to DefaultHetznerServerLimit (10).
	ServerLimit int32 `default:"10" json:"serverLimit,omitzero" jsonschema:"description=Maximum total Hetzner servers allowed for this cluster (control-planes + workers + autoscaler pool capacity). Set to 0 to use the default limit of 10,minimum=0"` //nolint:lll
	// AllowedCIDRs restricts public access to the Kubernetes API (6443) and Talos API (50000)
	// on control-plane nodes to the specified CIDR blocks. When empty, both APIs are open
	// to the entire internet (0.0.0.0/0 and ::/0). Applied to both the Hetzner Cloud Firewall
	// and the Talos OS-level ingress firewall for defense-in-depth.
	// Examples: ["203.0.113.0/24", "198.51.100.0/24"]
	AllowedCIDRs []string `json:"allowedCidrs,omitzero" jsonschema:"description=CIDR blocks allowed to access the Kubernetes API and Talos API on control-plane nodes. When empty defaults to 0.0.0.0/0 and ::/0 (open to all IPv4 and IPv6)."` //nolint:lll
	// AutoscalerNodePoolNames lists the node-group names configured in the
	// Kubernetes Cluster Autoscaler for this cluster. When non-empty, KSail
	// deletes servers labelled with hcloud/node-group=<name> during cluster
	// deletion so that autoscaler-managed nodes are cleaned up alongside
	// KSail-managed nodes.
	AutoscalerNodePoolNames []string `json:"autoscalerNodePoolNames,omitzero"`
	// NodeAutoscalerEnabled is set by the cluster factory when
	// spec.cluster.autoscaler.node.enabled (or deprecated nodeAutoscaling)
	// is true. The Talos provisioner reads this to decide whether to create
	// the cluster-autoscaler-config Secret during bootstrap.
	// Not user-facing in ksail.yaml — derived at runtime.
	NodeAutoscalerEnabled bool `json:"-"`
}

// OptionsOmni defines options specific to the Sidero Omni provider.
// These options are used when Provider is set to "Omni" for the Talos distribution.
type OptionsOmni struct {
	// Endpoint is the Omni API endpoint URL.
	// Example: "https://<account>.omni.siderolabs.io:443".
	Endpoint string `json:"endpoint,omitzero"`
	// EndpointEnvVar is the environment variable containing the Omni API endpoint URL.
	// When set, the value of this environment variable takes precedence over Endpoint.
	// Defaults to "OMNI_ENDPOINT".
	EndpointEnvVar string `default:"OMNI_ENDPOINT" json:"endpointEnvVar,omitzero"`
	// ServiceAccountKeyEnvVar is the environment variable containing the
	// base64-encoded Omni service account key.
	// Defaults to "OMNI_SERVICE_ACCOUNT_KEY".
	ServiceAccountKeyEnvVar string `default:"OMNI_SERVICE_ACCOUNT_KEY" json:"serviceAccountKeyEnvVar,omitzero"`
	// TalosVersion is the Talos version to use for the cluster in Omni.
	// Accepts values with or without the "v" prefix (e.g., "v1.11.2" or "1.11.2").
	// Generated templates normalize the value to include the "v" prefix.
	// This determines the Talos Linux version that Omni will deploy to machines.
	TalosVersion string `json:"talosVersion,omitzero"`
	// KubernetesVersion is the Kubernetes version to use for the cluster in Omni.
	// Accepts values with or without the "v" prefix (e.g., "v1.32.0" or "1.32.0").
	// Generated templates normalize the value to include the "v" prefix.
	// This determines the Kubernetes version that Omni will deploy.
	KubernetesVersion string `json:"kubernetesVersion,omitzero"`
	// MachineClass is the Omni machine class name to use for dynamic node allocation.
	// Machine classes are user-defined in the Omni dashboard and match machines
	// by labels (e.g., CPU, region, role). The specified class must exist in
	// the Omni account before cluster creation. The number of machines allocated
	// is derived from the controlPlanes and workers count in the cluster spec.
	// Mutually exclusive with Machines — set one or the other.
	// When neither MachineClass nor Machines is set, KSail automatically discovers
	// available (unallocated) machines in Omni and uses them for node allocation.
	MachineClass string `json:"machineClass,omitzero"`
	// Machines is a list of Omni machine UUIDs to use for static node allocation.
	// The first N machines are assigned as control planes (where N = controlPlanes count),
	// and the remaining machines are assigned as workers.
	// Mutually exclusive with MachineClass — set one or the other.
	// When neither MachineClass nor Machines is set, KSail automatically discovers
	// available (unallocated) machines in Omni and uses them for node allocation.
	Machines []string `json:"machines,omitzero"`
}

// --- AWS Options ---
//
// EKS cluster metadata (region, Kubernetes version, nodegroup shape, AMI
// family, etc.) lives in eks.yaml (eksctl.io/v1alpha5 ClusterConfig), which is
// the authoritative source of truth. KSail does not duplicate those fields in
// ksail.yaml; the EKS provisioner loads the eksctl ClusterConfig directly.

// HetznerNetworkCIDR returns the configured private-network CIDR for the
// given spec, falling back to DefaultHetznerNetworkCIDR when none is set.
func HetznerNetworkCIDR(spec Spec) string {
	if spec.Provider.Hetzner.NetworkCIDR != "" {
		return spec.Provider.Hetzner.NetworkCIDR
	}

	return DefaultHetznerNetworkCIDR
}

// ciliumVXLANPort is the UDP port used by Cilium for VXLAN encapsulation.
const ciliumVXLANPort = 8472

// defaultVXLANPort is the UDP port used by Flannel and Calico for VXLAN encapsulation.
const defaultVXLANPort = 4789

// HetznerCNIPort returns the VXLAN encapsulation UDP port for the configured CNI.
// Cilium uses port 8472; Flannel and Calico use 4789.
func HetznerCNIPort(spec Spec) int {
	if spec.Cluster.CNI == CNICilium {
		return ciliumVXLANPort
	}

	return defaultVXLANPort
}

// OptionsAWS defines options specific to the AWS cloud provider.
// Credentials are resolved via the standard AWS SDK v2 credential chain;
// the *EnvVar fields let users point KSail at non-standard environment
// variable names (mirrors the Hetzner/Omni pattern).
type OptionsAWS struct {
	// ProfileEnvVar is the environment variable containing the AWS shared-config profile name.
	// Defaults to "AWS_PROFILE".
	ProfileEnvVar string `default:"AWS_PROFILE" json:"profileEnvVar,omitzero"`
	// RegionEnvVar is the environment variable containing the AWS region.
	// When set, it overrides the region declared in eks.yaml.
	// Defaults to "AWS_REGION".
	RegionEnvVar string `default:"AWS_REGION" json:"regionEnvVar,omitzero"`
	// AccessKeyIDEnvVar is the environment variable containing a static AWS access key ID.
	// Defaults to "AWS_ACCESS_KEY_ID".
	AccessKeyIDEnvVar string `default:"AWS_ACCESS_KEY_ID" json:"accessKeyIdEnvVar,omitzero"`
	// SecretAccessKeyEnvVar is the environment variable containing a static AWS secret access key.
	// Defaults to "AWS_SECRET_ACCESS_KEY".
	SecretAccessKeyEnvVar string `default:"AWS_SECRET_ACCESS_KEY" json:"secretAccessKeyEnvVar,omitzero"`
	// SessionTokenEnvVar is the environment variable containing an AWS session token
	// (used with temporary credentials from STS).
	// Defaults to "AWS_SESSION_TOKEN".
	SessionTokenEnvVar string `default:"AWS_SESSION_TOKEN" json:"sessionTokenEnvVar,omitzero"`
}
