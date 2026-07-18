package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// --- Distribution-specific Options Types ---

// OptionsVanilla defines options specific to the Vanilla distribution (uses Kind with Docker provider).
// Node counts should be configured directly in kind.yaml.
type OptionsVanilla struct {
	// MirrorsDir is the directory for containerd host mirror configuration.
	// Defaults to "kind/mirrors" if not specified.
	MirrorsDir string `json:"mirrorsDir,omitzero"`
}

// OptionsEKS defines options specific to the EKS distribution.
type OptionsEKS struct {
	// ExperimentalInPlaceUpdates enables the experimental in-place update path for
	// `cluster update` on EKS: managed node-group scaling changes (desiredCapacity,
	// minSize, maxSize in eksctl.yaml) are applied via `eksctl scale nodegroup`
	// instead of the delete-and-recreate flow. Default false (recreate flow), as the
	// path has not yet been validated against a live EKS cluster.
	// Diff coverage: node-group scaling and instanceType (recreate-required) are
	// detected; other managed-node-group fields (amiFamily, volumes, labels, taints,
	// subnets, IAM, ...) are not reported by `eksctl get nodegroup` and are NOT
	// diffed — changes to them are not detected by `cluster update` while this
	// experimental path is enabled.
	ExperimentalInPlaceUpdates bool `json:"experimentalInPlaceUpdates,omitzero" jsonschema_description:"Experimental: apply managed node-group scaling changes in-place via 'eksctl scale nodegroup' during 'cluster update' instead of recreating the cluster. Default false. Diff coverage is limited to node-group scaling and instanceType; other managed-node-group fields are not diffed."` //nolint:lll

	// ExperimentalAWSLoadBalancerController enables installing the AWS Load
	// Balancer Controller as the cluster's LoadBalancer component when
	// spec.cluster.loadBalancer is Enabled. Default false: EKS keeps its
	// default in-tree Classic Load Balancer path and KSail installs nothing.
	// The controller's IAM permissions (node-role credentials; IRSA is #6232)
	// and subnet tags are prerequisites KSail does not create — see the
	// awslbcontroller installer package docs. Installed at cluster create and
	// by the operator's reconcile; enabling it on an existing cluster is not
	// yet detected by `cluster update`'s diff (#6231). Not yet validated
	// against a live EKS cluster.
	ExperimentalAWSLoadBalancerController bool `json:"experimentalAWSLoadBalancerController,omitzero" jsonschema_description:"Experimental: install the AWS Load Balancer Controller when spec.cluster.loadBalancer is Enabled, replacing the default in-tree Classic Load Balancer path. Default false (nothing is installed). IAM permissions and subnet tags are prerequisites KSail does not create."` //nolint:lll,tagliatelle // AWS keeps its conventional casing, like the sibling issuerURL/floatingIP fields
}

// OptionsTalos defines options specific to the Talos distribution.
type OptionsTalos struct {
	// Version pins the Talos OS (distribution) version used for cluster creation and
	// upgrades. When set, KSail uses this version as the Docker container image tag
	// and `cluster update` reconciles the cluster toward it (skipping downgrades).
	// Accepts values with or without the "v" prefix (e.g., "v1.11.2" or "1.11.2").
	// When empty, `cluster create` uses KSail's built-in default version and
	// `cluster update` follows the latest stable version available in the OCI
	// registry. Override per invocation with the --distribution-version flag
	// (precedence: flag > env > config > default).
	Version string `json:"version,omitzero"`
	// KubernetesVersion mirrors spec.cluster.kubernetesVersion for the provisioner.
	// It is populated by the cluster factory from the top-level field and is the raw
	// value (possibly with a "v" prefix); only its presence is significant. The
	// provisioner uses it to decide whether the user pinned a version: when empty,
	// `cluster update` renders the desired machine config at the version already
	// running on the cluster so an unrelated update never proposes an unrequested
	// upgrade. Not user-facing in ksail.yaml — derived at runtime.
	KubernetesVersion string `json:"-"`
	// ControlPlanes is the number of control-plane nodes (default: 1).
	//
	// Deprecated: use spec.cluster.controlPlanes. This field is kept as a
	// migration alias and emits a warning on load. Removal planned in a
	// future minor release.
	ControlPlanes int32 `json:"controlPlanes,omitzero" jsonschema:"minimum=0" jsonschema_description:"DEPRECATED: use spec.cluster.controlPlanes instead"` //nolint:lll
	// Workers is the number of worker nodes (default: 0).
	// When 0, scheduling is allowed on control-plane nodes.
	//
	// Deprecated: use spec.cluster.workers. This field is kept as a
	// migration alias and emits a warning on load. Removal planned in a
	// future minor release.
	Workers int32 `json:"workers,omitzero" jsonschema:"minimum=0" jsonschema_description:"DEPRECATED: use spec.cluster.workers instead"` //nolint:lll
	// Config is the path to the talosconfig file.
	// Defaults to "~/.talos/config".
	Config string `default:"~/.talos/config" json:"config,omitzero"`
	// ISO is the cloud provider's ISO/image ID for booting Talos Linux.
	// Only used when targeting cloud providers (e.g., Hetzner Cloud).
	// For Hetzner: See https://docs.hetzner.cloud/changelog for available Talos ISOs.
	// Defaults to 125127 (Talos Linux 1.12.4 x86). The prior default 122630
	// (Talos 1.11.2) was removed from Hetzner on 2026-03-18. For ARM, look up the
	// matching Talos ISO ID in the Hetzner Cloud Console (Images → ISOs).
	// When SchematicID is set, ISO is ignored in favour of a pre-built snapshot.
	ISO int64 `default:"125127" json:"iso,omitzero"`
	// SchematicID is the Talos factory schematic ID used to build a Hetzner snapshot image.
	// When set, KSail uploads a Talos OS disk snapshot using this schematic ID and Version
	// instead of booting from the cloud ISO specified in ISO.
	// Obtain a schematic ID from https://factory.talos.dev.
	// Only used when targeting cloud providers (e.g., Hetzner Cloud).
	SchematicID string `json:"schematicId,omitzero"`
	// Extensions lists Talos Image Factory official system extension names to include in the
	// node image. KSail automatically computes the Image Factory schematic ID from this list
	// and sets machine.install.image to the version-appropriate factory installer repository
	// (`factory.talos.dev/installer` before Talos 1.14, `factory.talos.dev/metal-installer`
	// for Talos 1.14+) using the Talos config bundle's existing install image tag.
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
	// during project init. When Enabled, generates an image-verification.yaml template
	// in the Talos patches directory with commented-out examples for keyless (Cosign/OIDC)
	// and public key verification rules. Requires Talos 1.13+.
	//
	// Deprecated: use spec.cluster.imageVerification. This field is kept as a
	// migration alias and emits a warning on load (the value is copied into the
	// cluster-level field). Removal planned in a future minor release.
	ImageVerification ImageVerification `json:"imageVerification,omitzero" jsonschema_description:"DEPRECATED: use spec.cluster.imageVerification instead"` //nolint:lll
	// DrainTimeout is the per-node pod-eviction budget for rolling node drains during
	// `cluster update` (rolling reboot and Hetzner server-type rolling-recreate). When
	// unset, KSail uses 10m. Increase it for clusters whose stateful workloads need
	// longer to evict gracefully — e.g. Longhorn volume rebuilds or database failovers
	// gated by PodDisruptionBudgets. A drain that exceeds this budget aborts the update;
	// re-run with --force-drain to delete pods bypassing PodDisruptionBudgets instead.
	// Override per invocation with --drain-timeout. Example: "15m".
	DrainTimeout metav1.Duration `json:"drainTimeout,omitzero"`
	// StorageHealthTimeout opts into a between-node storage-health gate during the
	// `cluster update` rolling reboot. When set to a positive duration, the roll waits
	// — up to this timeout — for the cluster's storage to return to a stable state
	// before draining the next node: generically, no PersistentVolume in phase Failed,
	// no PersistentVolumeClaim in phase Lost, and no VolumeAttachment with an
	// attach/detach error; plus, when a replicated node-local storage backend is
	// detected (Longhorn), no degraded or faulted replicated volume. This prevents
	// progressively faulting volumes whose replicas are spread one-per-node: without
	// the gate the roll advances as soon as a node reports Kubernetes Ready, so
	// rebooting consecutive replica holders before a rebuild completes can take every
	// replica of a volume down at once. Default off (unset / 0): behaviour is
	// unchanged. The gate only helps when replicas have spare capacity to rebuild
	// during the roll; on a fully drained pool with hard anti-affinity it times out
	// (naming the stuck volumes) rather than hanging. Example: "10m".
	//+kubebuilder:validation:XValidation:rule="self.matches('^[0-9]+(ns|us|µs|ms|s|m|h)$')"
	StorageHealthTimeout metav1.Duration `json:"storageHealthTimeout,omitzero"`
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
	// Credentials declares which environment variables hold the registry token for
	// each execution path. When set, it takes precedence over any password embedded
	// in the Registry spec.
	Credentials RegistryCredentials `json:"credentials,omitzero"`
}

// RegistryCredentials declares the environment variables that hold the registry
// token for each execution path. Following the KSail *EnvVar convention, each field
// contains the *name* of an environment variable rather than a token value, so
// credential resolution stays explicit, deterministic, and registry-agnostic.
//
// Resolution:
//   - CLI and publish (push) paths read CLITokenEnvVar, falling back to TokenEnvVar
//     only when the override is not configured.
//   - Cluster (pull) paths read ClusterTokenEnvVar, falling back to TokenEnvVar only
//     when the override is not configured.
//   - A configured override stays authoritative even when its environment variable is
//     missing or empty; resolution never falls back on process-environment state.
//   - When no field is set, the password embedded in the Registry spec is used.
//
// Splitting the two paths lets a cluster receive a least-privilege pull-only token
// while the CLI keeps a token that may also push.
type RegistryCredentials struct {
	// TokenEnvVar is the environment variable holding the registry token used by both
	// push and pull paths unless a path-specific override is configured.
	// Example: "GHCR_TOKEN".
	TokenEnvVar string `json:"tokenEnvVar,omitzero"`
	// CLITokenEnvVar overrides TokenEnvVar for CLI and publish (push) paths.
	// Example: "GHCR_PUSH_TOKEN".
	CLITokenEnvVar string `json:"cliTokenEnvVar,omitzero"`
	// ClusterTokenEnvVar overrides TokenEnvVar for cluster-side pull paths, so the token
	// persisted into the cluster can be pull-only.
	// Example: "GHCR_PULL_TOKEN".
	ClusterTokenEnvVar string `json:"clusterTokenEnvVar,omitzero"`
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
	FallbackLocations []string `json:"fallbackLocations,omitzero" jsonschema_description:"Alternative datacenter locations to try when server creation in the primary location fails due to resource unavailability. When empty defaults to nbg1 and hel1 (both in the eu-central network zone matching the default fsn1 primary location)."` //nolint:lll
	// PlacementGroupFallbackToNone allows automatic fallback to no placement group
	// when spread placement constraints cannot be satisfied (e.g., due to datacenter capacity).
	// When true and placement fails, retries server creation without a placement group.
	// Defaults to false to preserve HA guarantees; set to true for best-effort provisioning.
	PlacementGroupFallbackToNone bool `json:"placementGroupFallbackToNone,omitzero"`
	// FloatingIPEnabled provisions a Hetzner floating IP named "<cluster-name>-floating-ip"
	// and renders it into the generated configs as the stable Kubernetes/Talos API endpoint
	// (cluster endpoint plus certificate SANs; the control-plane node IPs stay in the SAN set
	// so direct node access keeps working). The floating IP survives any individual
	// control-plane server being deleted and recreated, so kubeconfigs and external consumers
	// keep a stable API address across control-plane rolls. On create the IP is attached to
	// the first control-plane server as the initial claim; KSail also renders a Talos VIP
	// block (machine.network.interfaces vip with hcloud API management) into the
	// control-plane configs, so the elected leader claims the address on every leader
	// change — no user-provided machine-config patch is needed. The hcloud API token
	// (from the configured tokenEnvVar) is embedded in the rendered control-plane machine
	// config, the trust surface Talos' hcloud VIP support prescribes. Defaults to false
	// (no floating IP; rendered configs are unchanged).
	FloatingIPEnabled bool `json:"floatingIPEnabled,omitzero" jsonschema_description:"Provision a Hetzner floating IP and render it as the stable Kubernetes/Talos API endpoint (endpoint + certificate SANs + a control-plane Talos VIP block for leader ownership handover; the hcloud API token is embedded in the control-plane machine config). Defaults to false."` //nolint:lll,tagliatelle // floatingIP casing matches the sibling *PublicIPv4 fields
	// FloatingIPLocation is the Hetzner location the floating IP is homed in. Homing only
	// affects routing latency, not which servers the IP can be assigned to. Defaults to the
	// cluster's Location.
	FloatingIPLocation string `json:"floatingIPLocation,omitzero" jsonschema_description:"Hetzner location the floating IP is homed in (routing latency only). Defaults to the cluster's location."` //nolint:lll,tagliatelle // floatingIP casing matches the sibling *PublicIPv4 fields
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
	ServerLimit int32 `default:"10" json:"serverLimit,omitzero" jsonschema:"minimum=0" jsonschema_description:"Maximum total Hetzner servers allowed for this cluster — the account/project quota. Validation rejects configs whose reachable total (control-planes + workers + pool capacity clamped by autoscaler.node.maxNodesTotal when set) exceeds it. Set to 0 to use the default limit of 10"` //nolint:lll
	// AllowedCIDRs restricts public access to the Kubernetes API (6443) and Talos API (50000)
	// on control-plane nodes to the specified CIDR blocks. When empty, both APIs are open
	// to the entire internet (0.0.0.0/0 and ::/0). Applied to both the Hetzner Cloud Firewall
	// and the Talos OS-level ingress firewall for defense-in-depth.
	// Examples: ["203.0.113.0/24", "198.51.100.0/24"]
	AllowedCIDRs []string `json:"allowedCidrs,omitzero" jsonschema_description:"CIDR blocks allowed to access the Kubernetes API and Talos API on control-plane nodes. When empty defaults to 0.0.0.0/0 and ::/0 (open to all IPv4 and IPv6)."` //nolint:lll
	// WorkerPublicIPv4 controls whether worker nodes are assigned a public IPv4 address.
	// nil (default) or true assigns a public IPv4 (billed by Hetzner). false provisions
	// IPv4-less workers; ksail then reaches their Talos API over the private network — which
	// requires ksail to have private-network reachability — and the nodes need egress via a
	// NAT gateway (or working IPv6) for image pulls, the Hetzner API, and cluster join.
	WorkerPublicIPv4 *bool `json:"workerPublicIPv4,omitzero" jsonschema_description:"Assign a public IPv4 to worker nodes. Defaults to true. Set false for IPv4-less workers reached over the private network (requires private-network reachability and NAT egress)."` //nolint:lll
	// WorkerPublicIPv6 controls whether worker nodes are assigned a public IPv6 address.
	// nil (default) or true assigns a public IPv6 (free on Hetzner). false disables it.
	WorkerPublicIPv6 *bool `json:"workerPublicIPv6,omitzero" jsonschema_description:"Assign a public IPv6 to worker nodes. Defaults to true."` //nolint:lll
	// ControlPlanePublicIPv4 controls whether control-plane nodes are assigned a public IPv4.
	// nil (default) or true assigns a public IPv4 (billed). false provisions IPv4-less control
	// planes; the kube/talos endpoint is then derived from the private-network IP so the cluster
	// is reachable only from inside the private network.
	ControlPlanePublicIPv4 *bool `json:"controlPlanePublicIPv4,omitzero" jsonschema_description:"Assign a public IPv4 to control-plane nodes. Defaults to true. Set false for IPv4-less control planes whose endpoint is the private-network IP (cluster reachable only from inside the private network)."` //nolint:lll
	// ControlPlanePublicIPv6 controls whether control-plane nodes are assigned a public IPv6.
	// nil (default) or true assigns a public IPv6 (free on Hetzner). false disables it.
	ControlPlanePublicIPv6 *bool `json:"controlPlanePublicIPv6,omitzero" jsonschema_description:"Assign a public IPv6 to control-plane nodes. Defaults to true."` //nolint:lll
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
	// AutoscalerNodePools carries the full autoscaler node pool definitions
	// (spec.cluster.autoscaler.node.pools) so the Talos provisioner can build
	// per-pool cloud-init worker configs and the HCLOUD_CLUSTER_CONFIG that
	// carries each pool's labels and taints. Not user-facing in ksail.yaml —
	// derived at runtime by the cluster factory.
	AutoscalerNodePools []NodePool `json:"-"`
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

// publicNetEnabled reports whether a *bool public-net toggle is enabled, treating
// nil (unset) as enabled — Hetzner's default is to assign both a public IPv4 and a
// public IPv6 to every server.
func publicNetEnabled(toggle *bool) bool {
	return toggle == nil || *toggle
}

// WorkerIPv4Enabled reports whether worker nodes should be assigned a public IPv4.
// Defaults to true when unset.
func (o *OptionsHetzner) WorkerIPv4Enabled() bool {
	return publicNetEnabled(o.WorkerPublicIPv4)
}

// WorkerIPv6Enabled reports whether worker nodes should be assigned a public IPv6.
// Defaults to true when unset.
func (o *OptionsHetzner) WorkerIPv6Enabled() bool {
	return publicNetEnabled(o.WorkerPublicIPv6)
}

// ControlPlaneIPv4Enabled reports whether control-plane nodes should be assigned a
// public IPv4. Defaults to true when unset.
func (o *OptionsHetzner) ControlPlaneIPv4Enabled() bool {
	return publicNetEnabled(o.ControlPlanePublicIPv4)
}

// ControlPlaneIPv6Enabled reports whether control-plane nodes should be assigned a
// public IPv6. Defaults to true when unset.
func (o *OptionsHetzner) ControlPlaneIPv6Enabled() bool {
	return publicNetEnabled(o.ControlPlanePublicIPv6)
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

// OptionsGCP defines options specific to the Google Cloud provider used by the
// GKE distribution. Credentials are resolved via Application Default
// Credentials (GOOGLE_APPLICATION_CREDENTIALS / gcloud ADC); the *EnvVar fields
// let users point KSail at non-standard environment variable names (mirrors
// the AWS/Hetzner/Omni pattern).
type OptionsGCP struct {
	// ProjectEnvVar is the environment variable containing the Google Cloud project ID.
	// Defaults to "GOOGLE_CLOUD_PROJECT".
	ProjectEnvVar string `default:"GOOGLE_CLOUD_PROJECT" json:"projectEnvVar,omitzero"`
	// LocationEnvVar is the environment variable containing the GKE location (zone or region).
	// Defaults to "GOOGLE_CLOUD_LOCATION".
	LocationEnvVar string `default:"GOOGLE_CLOUD_LOCATION" json:"locationEnvVar,omitzero"`
}

// OptionsAzure defines options specific to the Microsoft Azure provider used
// by the AKS distribution. Credentials are resolved via the Azure SDK's
// DefaultAzureCredential chain (environment, managed identity, Azure CLI);
// the *EnvVar fields let users point KSail at non-standard environment
// variable names (mirrors the AWS/GCP/Hetzner/Omni pattern).
type OptionsAzure struct {
	// SubscriptionIDEnvVar is the environment variable containing the Azure subscription ID.
	// Defaults to "AZURE_SUBSCRIPTION_ID".
	SubscriptionIDEnvVar string `default:"AZURE_SUBSCRIPTION_ID" json:"subscriptionIdEnvVar,omitzero"`
	// ResourceGroupEnvVar is the environment variable containing the Azure resource group
	// that hosts the cluster. Defaults to "AZURE_RESOURCE_GROUP". When neither the
	// environment variable nor a configured value provides a resource group, cluster-scoped
	// calls resolve it from the cluster's ARM ID via a subscription-wide list, and Create
	// requires it explicitly.
	ResourceGroupEnvVar string `default:"AZURE_RESOURCE_GROUP" json:"resourceGroupEnvVar,omitzero"`
}

// OptionsKubernetes defines options specific to the Kubernetes provider.
// The Kubernetes provider runs nested cluster nodes as pods inside an existing host cluster.
// It uses Gateway API (TCPRoute) to expose the nested cluster's API server.
type OptionsKubernetes struct {
	// Kubeconfig is the path to the kubeconfig for the host cluster.
	// Defaults to "~/.kube/config".
	Kubeconfig string `default:"~/.kube/config" json:"kubeconfig,omitzero"`
	// KubeconfigEnvVar is the environment variable containing the host kubeconfig path.
	// Defaults to "KSAIL_HOST_KUBECONFIG".
	KubeconfigEnvVar string `default:"KSAIL_HOST_KUBECONFIG" json:"kubeconfigEnvVar,omitzero"`
	// Context is the kubeconfig context for the host cluster.
	// When empty, uses the current context.
	Context string `json:"context,omitzero"`
	// ContextEnvVar is the environment variable containing the host kubeconfig context.
	// Defaults to "KSAIL_HOST_CONTEXT".
	ContextEnvVar string `default:"KSAIL_HOST_CONTEXT" json:"contextEnvVar,omitzero"`
	// GatewayClassName is the GatewayClass to use for exposing the nested API server.
	// Must reference a GatewayClass that exists on the host cluster.
	// When empty, the API is exposed via ClusterIP Service only (no external Gateway).
	GatewayClassName string `json:"gatewayClassName,omitzero"`
	// PodCIDR is the pod CIDR for the nested cluster.
	// Must not overlap with the host cluster's pod or service CIDRs.
	// Defaults to "10.64.0.0/16".
	PodCIDR string `default:"10.64.0.0/16" json:"podCidr,omitzero"`
	// ServiceCIDR is the service CIDR for the nested cluster.
	// Must not overlap with the host cluster's pod or service CIDRs.
	// Defaults to "10.128.0.0/16".
	ServiceCIDR string `default:"10.128.0.0/16" json:"serviceCidr,omitzero"`
	// Persistence defines storage persistence for the nested cluster's data directory.
	Persistence KubernetesPersistence `json:"persistence,omitzero"`
}

// KubernetesPersistence defines storage persistence configuration for the Kubernetes provider.
// When enabled, a PVC is created for the nested cluster's data directory to survive pod restarts.
// When disabled (default), emptyDir is used and clusters are fully ephemeral.
type KubernetesPersistence struct {
	// Enabled controls whether a PVC is used for the nested cluster's data directory.
	// When false (default), emptyDir is used and the cluster is ephemeral.
	Enabled bool `json:"enabled,omitzero"`
	// StorageClassName is the StorageClass to use for the PVC.
	// When empty, the cluster's default StorageClass is used.
	StorageClassName string `json:"storageClassName,omitzero"`
	// Size is the storage request size for the PVC.
	// Defaults to "20Gi".
	Size string `default:"20Gi" json:"size,omitzero"`
}
