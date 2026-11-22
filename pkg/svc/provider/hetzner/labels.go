package hetzner

import (
	"strconv"
)

// Label constants for identifying KSail-managed Hetzner resources.
// These labels are applied to servers, networks, firewalls, and placement groups.
const (
	// LabelOwned indicates the resource is managed by KSail.
	// Value is always [LabelOwnedValue] for KSail-managed resources.
	LabelOwned = "ksail.owned"

	// LabelOwnedValue is the value LabelOwned carries on KSail-managed resources.
	LabelOwnedValue = "true"

	// LabelClusterName identifies which cluster the resource belongs to.
	LabelClusterName = "ksail.cluster.name"

	// LabelNodeType identifies the role of the node: "controlplane" or "worker".
	LabelNodeType = "ksail.node.type"

	// LabelNodeIndex identifies the index of the node within its type.
	// For example, "0" for the first control-plane node.
	LabelNodeIndex = "ksail.node.index"

	// LabelAutoscalerNodeGroup is applied by the Kubernetes Cluster Autoscaler
	// (hcloud provider) to servers it creates. The value is the node group
	// (pool) name configured in the autoscaler deployment.
	LabelAutoscalerNodeGroup = "hcloud/node-group"
)

// Label constants for Talos snapshot images.
// These use the ksail.io/ prefix to distinguish them from server/infrastructure labels.
const (
	// LabelTalosVersion identifies the Talos version of the snapshot image.
	LabelTalosVersion = "ksail.io/talos-version"

	// LabelTalosSchematic identifies the Talos factory schematic ID of the snapshot image.
	// The stored value is truncated to maxLabelValueLen characters via SchematicLabelValue.
	LabelTalosSchematic = "ksail.io/talos-schematic"

	// LabelTalosCluster identifies which cluster created the snapshot image.
	LabelTalosCluster = "ksail.io/cluster"

	// LabelTalosSnapshotBuild identifies one snapshot build. The build's temporary server
	// and SSH key carry it, so a cancelled build can find and delete them. The snapshot
	// image carries it too.
	LabelTalosSnapshotBuild = "ksail.io/snapshot-build"

	// maxLabelValueLen is the maximum length of a Hetzner Cloud label value.
	// See https://github.com/hetznercloud/hcloud-go/blob/main/hcloud/label.go.
	maxLabelValueLen = 63
)

// SchematicLabelValue returns the schematic ID truncated to fit within
// Hetzner Cloud's 63-character label value limit. Talos factory schematic
// IDs are SHA256 hex digests (64 chars), which exceed the limit by one.
func SchematicLabelValue(schematicID string) string {
	if len(schematicID) > maxLabelValueLen {
		return schematicID[:maxLabelValueLen]
	}

	return schematicID
}

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
	// FloatingIPSuffix is appended to cluster name for the control-plane
	// floating IP naming.
	FloatingIPSuffix = "-floating-ip"
)

// ResourceLabels creates the standard label set for a KSail-managed resource.
func ResourceLabels(clusterName string) map[string]string {
	return map[string]string{
		LabelOwned:       LabelOwnedValue,
		LabelClusterName: clusterName,
	}
}

// LabelCCMServiceUID is the label hcloud-ccm sets on every load balancer it
// creates for a Kubernetes Service of type LoadBalancer.
const LabelCCMServiceUID = "hcloud-ccm/service-uid"

// ownedByCluster reports whether a resource's labels mark it as created by
// KSail for the named cluster. Both the ownership label and the cluster-name
// label must match. Cleanup paths that look a resource up by its conventional
// name must check this before deleting it, so a same-named resource made by
// another tool, or by KSail for another cluster, is never removed.
func ownedByCluster(labels map[string]string, clusterName string) bool {
	return labels[LabelOwned] == LabelOwnedValue && labels[LabelClusterName] == clusterName
}

// loadBalancerOwnedByCluster reports whether a load balancer attached to the
// cluster's KSail-owned network may be deleted with the cluster. KSail does not
// create load balancers itself; hcloud-ccm does, and marks each one with
// LabelCCMServiceUID. A load balancer carrying the cluster's KSail labels is
// accepted too. Any other load balancer on the network was attached by someone
// else and is kept.
func loadBalancerOwnedByCluster(labels map[string]string, clusterName string) bool {
	if ownedByCluster(labels, clusterName) {
		return true
	}

	_, createdByCCM := labels[LabelCCMServiceUID]

	return createdByCCM
}

// NodeLabels creates the complete label set for a cluster node.
func NodeLabels(clusterName string, nodeType string, index int) map[string]string {
	labels := ResourceLabels(clusterName)
	labels[LabelNodeType] = nodeType
	labels[LabelNodeIndex] = strconv.Itoa(index)

	return labels
}
