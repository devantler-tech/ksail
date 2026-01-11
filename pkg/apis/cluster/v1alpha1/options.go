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
