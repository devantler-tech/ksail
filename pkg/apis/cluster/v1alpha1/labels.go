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

// FinalizerName is added to Cluster resources so the operator can tear down the underlying cluster
// before the custom resource is removed from the API server. It lives here, beside the other
// ksail.io/* wire identifiers, so the controller and the REST API share one definition.
const FinalizerName = "ksail.io/finalizer"

// LastAppliedSpecAnnotation stores the JSON of the cluster spec the operator last provisioned, used
// as the drift-detection baseline on subsequent reconciles. It lives here, beside the other
// reserved ksail.io/* identifiers, so the controller (which writes it) and the REST API (which
// strips it from client-submitted objects, preventing users from forging the reconciler's baseline)
// share one definition and can never silently desync on a rename.
const LastAppliedSpecAnnotation = "ksail.io/last-applied-spec"

// LastAppliedComponentsAnnotation stores the JSON of the cluster spec the operator last installed
// components for, used as the baseline to detect components the spec has since dropped (flipped to
// None/Default) so the operator can uninstall them — the inverse of install-only reconciliation.
// It is distinct from LastAppliedSpecAnnotation on purpose: that one is the drift-detection baseline
// owned by the provisioner path (and rewritten as soon as an in-place update applies, before
// components reconcile), so reusing it would race the component diff. This one is owned solely by
// the component reconciler and written only after a successful component apply. Like the spec
// baseline it is reconciler-owned and stripped from client-submitted objects by the REST API, so a
// user cannot forge it.
const LastAppliedComponentsAnnotation = "ksail.io/last-applied-components"

// IsHostCluster reports whether this Cluster resource is the operator's self-registration of the
// cluster it runs on (see HostClusterLabel).
func (c *Cluster) IsHostCluster() bool {
	return c.Labels[HostClusterLabel] == "true"
}
