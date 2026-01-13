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
	// ControlPlanes is the number of control-plane nodes (default: 1).
	ControlPlanes int32 `json:"controlPlanes,omitzero" default:"1"`
	// Workers is the number of worker nodes (default: 0).
	// When 0, scheduling is allowed on control-plane nodes.
	Workers int32 `json:"workers,omitzero"`
	// Config is the path to the talosconfig file.
	// Defaults to "~/.talos/config".
	Config string `json:"config,omitzero" default:"~/.talos/config"`
	// ISO is the cloud provider's ISO/image ID for booting Talos Linux.
	// Only used when targeting cloud providers (e.g., Hetzner Cloud).
	// For Hetzner: See https://docs.hetzner.cloud/changelog for available Talos ISOs.
	// Defaults to 122630 (Talos Linux 1.11.2 x86). Use 122629 for ARM.
	ISO int64 `json:"iso,omitzero" default:"122630"`
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
	ControlPlaneServerType string `json:"controlPlaneServerType,omitzero" default:"cx23"`
	// WorkerServerType is the Hetzner server type for worker nodes.
	// Examples: "cx23" (x86), "cax11" (ARM), "cpx21" (AMD). Defaults to "cx23".
	WorkerServerType string `json:"workerServerType,omitzero" default:"cx23"`
	// Location is the Hetzner datacenter location.
	// Examples: "fsn1" (Falkenstein), "nbg1" (Nuremberg), "hel1" (Helsinki).
	// Defaults to "fsn1".
	Location string `json:"location,omitzero" default:"fsn1"`
	// NetworkName is the name of the private network to create or use.
	// If empty, a network named "<cluster-name>-network" will be created.
	NetworkName string `json:"networkName,omitzero"`
	// NetworkCIDR is the CIDR block for the private network.
	// Defaults to "10.0.0.0/16".
	NetworkCIDR string `json:"networkCidr,omitzero" default:"10.0.0.0/16"`
	// SSHKeyName is the name of the SSH key to use for server access.
	// The key must already exist in the Hetzner Cloud project.
	// If empty, no SSH key is attached (only Talos API access).
	SSHKeyName string `json:"sshKeyName,omitzero"`
	// TokenEnvVar is the environment variable containing the Hetzner API token.
	// Defaults to "HCLOUD_TOKEN".
	TokenEnvVar string `json:"tokenEnvVar,omitzero" default:"HCLOUD_TOKEN"`
	// PlacementGroup is the name of the placement group for server distribution.
	// If empty, a placement group named "<cluster-name>-placement" will be created
	// with "spread" strategy for high availability.
	PlacementGroup string `json:"placementGroup,omitzero"`
}
