package v1alpha1

import (
	"fmt"
	"slices"
	"strings"
)

// Distribution defines the distribution options for a KSail cluster.
type Distribution string

const (
	// DistributionVanilla is the vanilla Kubernetes distribution (uses Kind with Docker provider).
	DistributionVanilla Distribution = "Vanilla"
	// DistributionK3s is the K3s distribution.
	DistributionK3s Distribution = "K3s"
	// DistributionTalos is the Talos distribution.
	DistributionTalos Distribution = "Talos"
	// DistributionVCluster is the vCluster distribution (uses Vind/Docker driver).
	DistributionVCluster Distribution = "VCluster"
	// DistributionKWOK is the KWOK distribution (simulated Kubernetes cluster).
	DistributionKWOK Distribution = "KWOK"
)

// ProvidesCDIByDefault returns true if the distribution enables CDI by default.
// Talos 1.13+ enables CDI (Container Device Interface) by default via machine.features.enableCDI.
// Vanilla, K3s, and VCluster do not enable CDI by default.
func (d *Distribution) ProvidesCDIByDefault() bool {
	switch *d {
	case DistributionTalos:
		return true
	case DistributionVanilla, DistributionK3s, DistributionVCluster, DistributionKWOK:
		return false
	default:
		return false
	}
}

// ProvidesMetricsServerByDefault returns true if the distribution includes metrics-server by default.
// K3s includes metrics-server.
// Vanilla, Talos, and VCluster (Vind with Distro: k8s) do not.
func (d *Distribution) ProvidesMetricsServerByDefault() bool {
	switch *d {
	case DistributionK3s:
		return true
	case DistributionVanilla, DistributionTalos, DistributionVCluster, DistributionKWOK:
		return false
	default:
		return false
	}
}

// ProvidesStorageByDefault returns true if the distribution includes a storage provisioner by default.
// K3s includes local-path-provisioner.
// Vanilla, Talos, and VCluster (Vind with Distro: k8s) do not have a default storage class.
func (d *Distribution) ProvidesStorageByDefault() bool {
	switch *d {
	case DistributionK3s:
		return true
	case DistributionVanilla, DistributionTalos, DistributionVCluster, DistributionKWOK:
		return false
	default:
		return false
	}
}

// ProvidesCSIByDefault returns true if the distribution × provider combination includes CSI by default.
// - K3s includes local-path-provisioner by default (regardless of provider)
// - Talos × Hetzner uses Hetzner CSI driver by default
// - Vanilla, VCluster (Vind with Distro: k8s), and Talos × Docker do not have a default CSI.
func (d *Distribution) ProvidesCSIByDefault(provider Provider) bool {
	switch *d {
	case DistributionK3s:
		// K3s always includes local-path-provisioner
		return true
	case DistributionTalos:
		// Talos × Hetzner provides Hetzner CSI by default
		return provider == ProviderHetzner
	case DistributionVanilla, DistributionVCluster, DistributionKWOK:
		// Vanilla (Kind), VCluster (Vind with Distro: k8s), and KWOK do not provide CSI by default
		return false
	default:
		return false
	}
}

// ProvidesLoadBalancerByDefault returns true if the distribution × provider combination
// includes LoadBalancer support by default.
//   - K3s includes ServiceLB (Klipper-LB) by default (regardless of provider)
//   - Talos × Hetzner: returns true because hcloud-ccm can provide LoadBalancer
//     support, but it is not pre-installed — KSail installs it when LoadBalancer
//     is Default or Enabled (see NeedsLoadBalancerInstall special case)
//   - VCluster delegates LoadBalancer to the host cluster
//   - Vanilla and Talos × Docker do not have default LoadBalancer support.
func (d *Distribution) ProvidesLoadBalancerByDefault(provider Provider) bool {
	switch *d {
	case DistributionK3s, DistributionVCluster:
		// K3s always includes ServiceLB (Klipper-LB)
		// VCluster delegates LoadBalancer to the host cluster
		return true
	case DistributionTalos:
		// Talos × Hetzner: hcloud-ccm provides LB support (installed by KSail)
		return provider == ProviderHetzner
	case DistributionVanilla, DistributionKWOK:
		// Vanilla (Kind) and KWOK do not provide LoadBalancer by default
		return false
	default:
		return false
	}
}

// Set for Distribution (pflag.Value interface).
func (d *Distribution) Set(value string) error {
	for _, dist := range ValidDistributions() {
		if strings.EqualFold(value, string(dist)) {
			*d = dist

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s, %s, %s)",
		ErrInvalidDistribution,
		value,
		DistributionVanilla,
		DistributionK3s,
		DistributionTalos,
		DistributionVCluster,
		DistributionKWOK,
	)
}

// IsValid checks if the distribution value is supported.
func (d *Distribution) IsValid() bool {
	return slices.Contains(ValidDistributions(), *d)
}

// String returns the string representation of the Distribution.
func (d *Distribution) String() string {
	return string(*d)
}

// Type returns the type of the Distribution.
func (d *Distribution) Type() string {
	return "Distribution"
}

// Default returns the default value for Distribution (Vanilla).
func (d *Distribution) Default() any {
	return DistributionVanilla
}

// ValidValues returns all valid Distribution values as strings.
func (d *Distribution) ValidValues() []string {
	return []string{
		string(DistributionVanilla),
		string(DistributionK3s),
		string(DistributionTalos),
		string(DistributionVCluster),
		string(DistributionKWOK),
	}
}

// ContextName returns the kubeconfig context name for a given cluster name.
// Each distribution has its own context naming convention:
//   - Vanilla: kind-<name>
//   - K3s: k3d-<name>
//   - Talos: admin@<name>
//
// Returns empty string if name is empty.
func (d *Distribution) ContextName(clusterName string) string {
	if clusterName == "" {
		return ""
	}

	switch *d {
	case DistributionVanilla:
		return "kind-" + clusterName
	case DistributionK3s:
		return "k3d-" + clusterName
	case DistributionTalos:
		return "admin@" + clusterName
	case DistributionVCluster:
		return "vcluster-docker_" + clusterName
	case DistributionKWOK:
		return "kwok-" + clusterName
	default:
		return ""
	}
}

// DefaultClusterName returns the default cluster name for a distribution.
// Each distribution has its own default naming convention:
//   - Vanilla: "kind"
//   - K3s: "k3d-default"
//   - Talos: "talos-default"
//
// Returns "kind" for unknown distributions.
func (d *Distribution) DefaultClusterName() string {
	switch *d {
	case DistributionVanilla:
		return "kind"
	case DistributionK3s:
		return "k3d-default"
	case DistributionTalos:
		return "talos-default"
	case DistributionVCluster:
		return "vcluster-default"
	case DistributionKWOK:
		return "kwok-default"
	default:
		return "kind"
	}
}
