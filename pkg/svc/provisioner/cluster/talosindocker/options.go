package talosindockerprovisioner

import "path/filepath"

// Default configuration values for TalosInDocker clusters.
const (
	// DefaultClusterName is the default name for TalosInDocker clusters.
	DefaultClusterName = "talos-default"
	// DefaultPatchesDir is the default directory for Talos patches.
	DefaultPatchesDir = "talos"
	// DefaultTalosImage is the default Talos container image.
	DefaultTalosImage = "ghcr.io/siderolabs/talos:latest"
	// DefaultControlPlaneNodes is the default number of control-plane nodes.
	DefaultControlPlaneNodes = 1
	// DefaultWorkerNodes is the default number of worker nodes.
	DefaultWorkerNodes = 0
	// DefaultNetworkCIDR is the default CIDR for the cluster network.
	DefaultNetworkCIDR = "10.5.0.0/24"
	// DefaultKubernetesVersion is the default Kubernetes version.
	DefaultKubernetesVersion = "1.32.0"
)

// Options holds runtime options for a TalosInDocker provisioner.
// Unlike TalosInDockerConfig which was also responsible for loading Talos patches,
// Options only contains provisioning settings. The Talos machine configuration
// is now loaded separately via the talos config-manager.
type Options struct {
	// TalosImage is the Talos container image to use.
	TalosImage string

	// ControlPlaneNodes is the number of control-plane nodes.
	ControlPlaneNodes int

	// WorkerNodes is the number of worker nodes.
	WorkerNodes int

	// NetworkCIDR is the CIDR for the cluster network.
	NetworkCIDR string

	// KubeconfigPath is the path to write the kubeconfig.
	KubeconfigPath string

	// TalosconfigPath is the path to write the talosconfig.
	TalosconfigPath string
}

// NewOptions creates new Options with default values.
func NewOptions() *Options {
	return &Options{
		TalosImage:        DefaultTalosImage,
		ControlPlaneNodes: DefaultControlPlaneNodes,
		WorkerNodes:       DefaultWorkerNodes,
		NetworkCIDR:       DefaultNetworkCIDR,
	}
}

// WithTalosImage sets the Talos container image.
func (o *Options) WithTalosImage(image string) *Options {
	if image != "" {
		o.TalosImage = image
	}

	return o
}

// WithControlPlaneNodes sets the number of control-plane nodes.
func (o *Options) WithControlPlaneNodes(count int) *Options {
	if count > 0 {
		o.ControlPlaneNodes = count
	}

	return o
}

// WithWorkerNodes sets the number of worker nodes.
func (o *Options) WithWorkerNodes(count int) *Options {
	if count >= 0 {
		o.WorkerNodes = count
	}

	return o
}

// WithNetworkCIDR sets the network CIDR for the cluster.
func (o *Options) WithNetworkCIDR(cidr string) *Options {
	if cidr != "" {
		o.NetworkCIDR = cidr
	}

	return o
}

// WithKubeconfigPath sets the kubeconfig output path.
func (o *Options) WithKubeconfigPath(path string) *Options {
	if path != "" {
		o.KubeconfigPath = path
	}

	return o
}

// WithTalosconfigPath sets the talosconfig output path.
func (o *Options) WithTalosconfigPath(path string) *Options {
	if path != "" {
		o.TalosconfigPath = path
	}

	return o
}

// PatchDirs returns the patch directory structure for a given base patches directory.
type PatchDirs struct {
	Root          string
	Cluster       string
	ControlPlanes string
	Workers       string
}

// NewPatchDirs creates a PatchDirs structure from a root patches directory.
func NewPatchDirs(patchesDir string) PatchDirs {
	if patchesDir == "" {
		patchesDir = DefaultPatchesDir
	}

	return PatchDirs{
		Root:          patchesDir,
		Cluster:       filepath.Join(patchesDir, "cluster"),
		ControlPlanes: filepath.Join(patchesDir, "control-planes"),
		Workers:       filepath.Join(patchesDir, "workers"),
	}
}
