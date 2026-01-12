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
}

// LocalRegistry defines options for the host-local OCI registry integration.
type LocalRegistry struct {
	// Enabled controls whether the local registry is provisioned and managed.
	// Defaults to false (disabled).
	Enabled bool `json:"enabled,omitzero"`
	// HostPort is the port on the host machine to expose the registry on.
	// Defaults to 5050.
	HostPort int32 `json:"hostPort,omitzero" default:"5050"`
}

// OptionsHetzner defines options specific to the Hetzner Cloud provider.
// These options are used when Provider is set to "Hetzner" for the Talos distribution.
type OptionsHetzner struct {
	// ControlPlaneServerType is the Hetzner server type for control-plane nodes.
	// Examples: "cax11" (ARM), "cx22" (x86), "cpx21" (AMD). Defaults to "cax11".
	ControlPlaneServerType string `json:"controlPlaneServerType,omitzero" default:"cax11"`
	// WorkerServerType is the Hetzner server type for worker nodes.
	// Examples: "cax11" (ARM), "cx22" (x86), "cpx21" (AMD). Defaults to "cax11".
	WorkerServerType string `json:"workerServerType,omitzero" default:"cax11"`
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
	// ISOID is the Hetzner ISO ID for the Talos Linux bootable ISO.
	// Hetzner provides Talos Linux as a public ISO with qemu-guest-agent.
	// See https://docs.hetzner.cloud/changelog for available versions.
	// Defaults to 122629 (Talos Linux 1.11.2 ARM).
	// Use 122630 for x86 architecture.
	ISOID int64 `json:"isoId,omitzero" default:"122629"`
}
