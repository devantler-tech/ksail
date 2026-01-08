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

// TalosProvider defines the provider backend for running Talos clusters.
type TalosProvider string

const (
	// TalosProviderDocker runs Talos nodes as Docker containers.
	TalosProviderDocker TalosProvider = "Docker"
)

// Default returns the default value for TalosProvider (Docker).
func (t *TalosProvider) Default() any {
	return TalosProviderDocker
}

// OptionsTalos defines options specific to the Talos distribution.
type OptionsTalos struct {
	// Provider specifies the backend for running Talos nodes (default: Docker).
	Provider TalosProvider `json:"provider,omitzero"`
	// ControlPlanes is the number of control-plane nodes (default: 1).
	ControlPlanes int32 `json:"controlPlanes,omitzero" default:"1"` //nolint:tagalign
	// Workers is the number of worker nodes (default: 0).
	// When 0, scheduling is allowed on control-plane nodes.
	Workers int32 `json:"workers,omitzero"`
}

// OptionsCilium defines options for the Cilium CNI.
type OptionsCilium struct {
	// Add any specific fields for the Cilium CNI here.
}

// OptionsCalico defines options for the Calico CNI.
type OptionsCalico struct {
	// Add any specific fields for the Calico CNI here.
}

// OptionsFlux defines options for the Flux deployment tool.
type OptionsFlux struct {
	// Add any specific fields for the Flux tool here.
}

// OptionsArgoCD defines options for the ArgoCD deployment tool.
type OptionsArgoCD struct {
	// Add any specific fields for the ArgoCD tool here.
}

// OptionsLocalRegistry defines options for the host-local OCI registry integration.
type OptionsLocalRegistry struct {
	HostPort int32 `default:"5050" json:"hostPort,omitzero"`
}

// OptionsHelm defines options for the Helm tool.
type OptionsHelm struct {
	// Add any specific fields for the Helm tool here.
}

// OptionsKustomize defines options for the Kustomize tool.
type OptionsKustomize struct {
	// Add any specific fields for the Kustomize tool here.
}
