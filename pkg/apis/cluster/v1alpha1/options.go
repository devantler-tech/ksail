package v1alpha1

// --- Distribution-specific Options Types ---

// OptionsKind defines options specific to the Kind distribution.
// Node counts should be configured directly in kind.yaml.
type OptionsKind struct {
	// MirrorsDir is the directory for containerd host mirror configuration.
	// Defaults to "kind/mirrors" if not specified.
	MirrorsDir string `json:"mirrorsDir,omitzero"`
}

// OptionsK3d defines options specific to the K3d distribution.
// Node counts should be configured directly in k3d.yaml.
type OptionsK3d struct {
	// Add any specific fields for the K3d distribution here.
}

// OptionsTalos defines options specific to the Talos distribution.
type OptionsTalos struct {
	// ControlPlanes is the number of control-plane nodes (default: 1).
	ControlPlanes int32 `json:"controlPlanes,omitzero" default:"1"`
	// Workers is the number of worker nodes (default: 0).
	// When 0, scheduling is allowed on control-plane nodes.
	Workers int32 `json:"workers,omitzero"`
}

// OptionsEKS defines options specific to the EKS Anywhere distribution.
// Node counts should be configured directly in eks.yaml.
type OptionsEKS struct {
	// Add any specific fields for the EKS distribution here.
}

// OptionsLocalRegistry defines options for the host-local OCI registry integration.
type OptionsLocalRegistry struct {
	HostPort int32 `default:"5050" json:"hostPort,omitzero"`
}
