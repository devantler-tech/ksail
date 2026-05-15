package kubernetes

// Label constants for identifying KSail-managed resources in the host cluster.
const (
	// LabelManagedBy identifies resources managed by KSail.
	LabelManagedBy = "ksail.io/managed-by"

	// LabelManagedByValue is the value for the managed-by label.
	LabelManagedByValue = "ksail"

	// LabelClusterName identifies which nested cluster a resource belongs to.
	LabelClusterName = "ksail.io/cluster"

	// LabelNodeRole identifies the role of a nested cluster node pod.
	LabelNodeRole = "ksail.io/node-role"

	// LabelDistribution identifies the Kubernetes distribution of the nested cluster.
	LabelDistribution = "ksail.io/distribution"

	// RoleControlPlane is the label value for control-plane nodes.
	RoleControlPlane = "control-plane"

	// RoleWorker is the label value for worker nodes.
	RoleWorker = "worker"
)

// NamespacePrefix is the prefix for KSail-managed nested cluster namespaces (e.g., "ksail-mycluster").
// Other provisioners (k3k, vCluster) may use their own namespace prefixes alongside this one.
const NamespacePrefix = "ksail-"

// NamespaceName returns the namespace name for a nested cluster.
func NamespaceName(clusterName string) string {
	return NamespacePrefix + clusterName
}

// CommonLabels returns the standard set of labels applied to all managed resources.
func CommonLabels(clusterName string) map[string]string {
	return map[string]string{
		LabelManagedBy:   LabelManagedByValue,
		LabelClusterName: clusterName,
	}
}

// NodeLabels returns labels for a nested cluster node pod.
func NodeLabels(clusterName, role, distribution string) map[string]string {
	labels := CommonLabels(clusterName)
	labels[LabelNodeRole] = role
	labels[LabelDistribution] = distribution

	return labels
}
