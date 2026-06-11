package v1alpha1

// ManagedNamespaceLabel marks a Namespace that the KSail operator created on demand to hold a
// Cluster resource. The operator only ever deletes namespaces carrying this label, so namespaces
// that already existed (e.g. "default" or user-managed namespaces) are never removed.
const ManagedNamespaceLabel = "ksail.io/managed-namespace"

// HostClusterLabel marks the Cluster resource the operator self-registers to represent the cluster
// it runs ON (the hub), following the pattern of Rancher's "local" cluster and Argo CD's
// "in-cluster" destination. The label is reserved: the operator never provisions, updates, or
// deletes the underlying cluster for a resource carrying it — it only observes status and serves
// resource browsing through its own in-cluster credentials — and the REST API rejects lifecycle
// mutations on it.
const HostClusterLabel = "ksail.io/host-cluster"

// IsHostCluster reports whether this Cluster resource is the operator's self-registration of the
// cluster it runs on (see HostClusterLabel).
func (c *Cluster) IsHostCluster() bool {
	return c.Labels[HostClusterLabel] == "true"
}
