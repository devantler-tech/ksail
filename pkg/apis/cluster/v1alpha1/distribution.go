package v1alpha1

import "slices"

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
	// DistributionEKS is the Amazon EKS distribution (managed Kubernetes on AWS).
	// Cluster creation is delegated to the eksctl CLI; Upgrade, Delete and nodegroup
	// scaling are handled via the embedded eksctl Go libraries.
	DistributionEKS Distribution = "EKS"
)

// ValidDistributions returns supported distribution values.
func ValidDistributions() []Distribution {
	return []Distribution{
		DistributionVanilla,
		DistributionK3s,
		DistributionTalos,
		DistributionVCluster,
		DistributionKWOK,
		DistributionEKS,
	}
}

// ProvidesCDIByDefault returns true if the distribution enables CDI by default.
// Talos 1.13+ enables CDI (Container Device Interface) by default via machine.features.enableCDI.
// Vanilla, K3s, and VCluster do not enable CDI by default.
func (d *Distribution) ProvidesCDIByDefault() bool {
	meta, _ := distributionMetaFor(*d)

	return meta.providesCDI
}

// ProvidesMetricsServerByDefault returns true if the distribution includes metrics-server by default.
// K3s includes metrics-server.
// Vanilla, Talos, VCluster, KWOK, and EKS do not.
func (d *Distribution) ProvidesMetricsServerByDefault() bool {
	meta, _ := distributionMetaFor(*d)

	return meta.providesMetricsServer
}

// ProvidesStorageByDefault returns true if the distribution includes a storage provisioner by default.
// K3s includes local-path-provisioner; EKS includes the Amazon EBS CSI addon by default
// when scaffolded via eksctl.
// Vanilla, Talos, VCluster (Vind with Distro: k8s), and KWOK do not have a default storage class.
func (d *Distribution) ProvidesStorageByDefault() bool {
	meta, _ := distributionMetaFor(*d)

	return meta.providesStorage
}

// ProvidesCSIByDefault returns true if the distribution × provider combination includes CSI by default.
// - K3s includes local-path-provisioner by default (regardless of provider)
// - Talos × Hetzner uses Hetzner CSI driver by default
// - EKS × AWS scaffolds the Amazon EBS CSI driver as an eksctl addon
// - Vanilla, VCluster (Vind with Distro: k8s), and Talos × Docker do not have a default CSI.
func (d *Distribution) ProvidesCSIByDefault(provider Provider) bool {
	meta, _ := distributionMetaFor(*d)

	return meta.csiByDefault.has(provider)
}

// ProvidesLoadBalancerByDefault returns true if the distribution × provider combination
// includes LoadBalancer support by default.
//   - K3s includes ServiceLB (Klipper-LB) by default (regardless of provider)
//   - Talos × Hetzner: returns true because hcloud-ccm can provide LoadBalancer
//     support, but it is not pre-installed — KSail installs it when LoadBalancer
//     is Default or Enabled (see NeedsLoadBalancerInstall special case)
//   - VCluster delegates LoadBalancer to the host cluster
//   - EKS × AWS: AWS Load Balancer Controller + native ELB Service integration
//   - Vanilla and Talos × Docker do not have default LoadBalancer support.
func (d *Distribution) ProvidesLoadBalancerByDefault(provider Provider) bool {
	meta, _ := distributionMetaFor(*d)

	return meta.loadBalancerByDefault.has(provider)
}

// Set for Distribution (pflag.Value interface).
func (d *Distribution) Set(value string) error {
	return setEnum(d, value, ValidDistributions(), ErrInvalidDistribution)
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
	return validValueStrings(ValidDistributions())
}

// ContextName returns the kubeconfig context name for a given cluster name.
// Each distribution has its own context naming convention:
//   - Vanilla: kind-<name>
//   - K3s: k3d-<name>
//   - Talos: admin@<name>
//   - VCluster: vcluster-docker_<name>
//   - KWOK: kwok-<name>
//   - EKS: <name>.eksctl.io — eksctl writes kubeconfig contexts as
//     <iam-user-or-role>@<cluster>.<region>.eksctl.io; the IAM identity is unknown at
//     scaffold time, so only the suffix is returned. Callers that need the full
//     context should query the kubeconfig after cluster creation.
//
// Returns empty string if name is empty or the distribution is unknown.
func (d *Distribution) ContextName(clusterName string) string {
	if clusterName == "" {
		return ""
	}

	meta, found := distributionMetaFor(*d)
	if !found {
		return ""
	}

	return meta.contextPrefix + clusterName + meta.contextSuffix
}

// DefaultClusterName returns the default cluster name for a distribution.
// Each distribution has its own default naming convention:
//   - Vanilla: "kind"
//   - K3s: "k3d-default"
//   - Talos: "talos-default"
//
// Returns "kind" for unknown distributions.
func (d *Distribution) DefaultClusterName() string {
	meta, found := distributionMetaFor(*d)
	if !found {
		return "kind"
	}

	return meta.defaultClusterName
}
