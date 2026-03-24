package talosprovisioner

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
)

// Default provisioner-specific values.
// Talos configuration defaults (cluster name, image, network CIDR, k8s version)
// are defined in the talos configmanager package.
const (
	// DefaultControlPlaneNodes is the default number of control-plane nodes.
	DefaultControlPlaneNodes = 1
	// DefaultWorkerNodes is the default number of worker nodes.
	DefaultWorkerNodes = 0
)

// Options holds runtime options for a Talos provisioner.
// Unlike TalosConfig which was also responsible for loading Talos patches,
// Options only contains provisioning settings. The Talos machine configuration
// is now loaded separately via the talos configmanager.
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

	// SkipCNIChecks indicates whether to skip CNI-dependent cluster checks
	// (CoreDNS, kube-proxy) during bootstrap. This should be set to true when
	// KSail will install a custom CNI (Cilium, Calico) after cluster creation,
	// as pods cannot start until the CNI is installed.
	SkipCNIChecks bool

	// ExtraPortMappings defines additional port mappings from Docker containers to the host.
	// Only used with the Docker provider. Each entry is in the Talos SDK format:
	// "[hostIP:]hostPort:containerPort/protocol".
	ExtraPortMappings []string
}

// NewOptions creates new Options with default values.
func NewOptions() *Options {
	return &Options{
		TalosImage:        talosconfigmanager.DefaultTalosImage,
		ControlPlaneNodes: DefaultControlPlaneNodes,
		WorkerNodes:       DefaultWorkerNodes,
		NetworkCIDR:       talosconfigmanager.DefaultNetworkCIDR,
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

// WithSkipCNIChecks sets whether to skip CNI-dependent cluster checks.
// This should be true when KSail will install a custom CNI after cluster creation.
func (o *Options) WithSkipCNIChecks(skip bool) *Options {
	o.SkipCNIChecks = skip

	return o
}

// WithExtraPortMappings sets the extra port mappings for Docker containers.
func (o *Options) WithExtraPortMappings(ports []string) *Options {
	o.ExtraPortMappings = ports

	return o
}

// maxPort is the maximum valid port number.
const maxPort = 65535

// PortMappingsToStrings converts API PortMapping structs to Talos SDK port strings.
// Format: "[hostIP:]hostPort:containerPort/protocol".
// Returns an error if any mapping has an invalid container port (must be 1-65535),
// host port (must be 0-65535), or protocol (must be "TCP" or "UDP").
func PortMappingsToStrings(mappings []v1alpha1.PortMapping) ([]string, error) {
	if len(mappings) == 0 {
		return nil, nil
	}

	ports := make([]string, 0, len(mappings))

	for portMappingIndex, pm := range mappings {
		if pm.ContainerPort < 1 || pm.ContainerPort > maxPort {
			return nil, fmt.Errorf(
				"extraPortMappings[%d]: containerPort %d is out of range (must be 1-%d)",
				portMappingIndex, pm.ContainerPort, maxPort,
			)
		}

		if pm.HostPort < 0 || pm.HostPort > maxPort {
			return nil, fmt.Errorf(
				"extraPortMappings[%d]: hostPort %d is out of range (must be 0-%d)",
				portMappingIndex, pm.HostPort, maxPort,
			)
		}

		protocol := strings.ToLower(pm.Protocol)
		if protocol == "" {
			protocol = "tcp"
		}

		if protocol != "tcp" && protocol != "udp" {
			return nil, fmt.Errorf(
				"extraPortMappings[%d]: protocol %q is invalid (must be TCP or UDP)",
				portMappingIndex, pm.Protocol,
			)
		}

		if pm.HostPort > 0 {
			ports = append(ports, fmt.Sprintf("%d:%d/%s", pm.HostPort, pm.ContainerPort, protocol))
		} else {
			ports = append(ports, fmt.Sprintf("0:%d/%s", pm.ContainerPort, protocol))
		}
	}

	return ports, nil
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
		patchesDir = talosconfigmanager.DefaultPatchesDir
	}

	return PatchDirs{
		Root:          patchesDir,
		Cluster:       filepath.Join(patchesDir, "cluster"),
		ControlPlanes: filepath.Join(patchesDir, "control-planes"),
		Workers:       filepath.Join(patchesDir, "workers"),
	}
}
