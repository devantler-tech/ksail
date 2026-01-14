package hetzner

import "strconv"

// Label constants for identifying KSail-managed Hetzner resources.
// These labels are applied to servers, networks, firewalls, and placement groups.
const (
	// LabelOwned indicates the resource is managed by KSail.
	// Value is always "true" for KSail-managed resources.
	LabelOwned = "ksail.owned"

	// LabelClusterName identifies which cluster the resource belongs to.
	LabelClusterName = "ksail.cluster.name"

	// LabelNodeType identifies the role of the node: "controlplane" or "worker".
	LabelNodeType = "ksail.node.type"

	// LabelNodeIndex identifies the index of the node within its type.
	// For example, "0" for the first control-plane node.
	LabelNodeIndex = "ksail.node.index"
)

// Node type values for LabelNodeType.
const (
	// NodeTypeControlPlane indicates a control-plane node.
	NodeTypeControlPlane = "controlplane"
	// NodeTypeWorker indicates a worker node.
	NodeTypeWorker = "worker"
)

// DefaultResourceSuffix constants for naming Hetzner resources.
const (
	// NetworkSuffix is appended to cluster name for network naming.
	NetworkSuffix = "-network"
	// FirewallSuffix is appended to cluster name for firewall naming.
	FirewallSuffix = "-firewall"
	// PlacementGroupSuffix is appended to cluster name for placement group naming.
	PlacementGroupSuffix = "-placement"
)

// ResourceLabels creates the standard label set for a KSail-managed resource.
func ResourceLabels(clusterName string) map[string]string {
	return map[string]string{
		LabelOwned:       "true",
		LabelClusterName: clusterName,
	}
}

// NodeLabels creates the complete label set for a cluster node.
func NodeLabels(clusterName string, nodeType string, index int) map[string]string {
	labels := ResourceLabels(clusterName)
	labels[LabelNodeType] = nodeType
	labels[LabelNodeIndex] = strconv.Itoa(index)

	return labels
}
