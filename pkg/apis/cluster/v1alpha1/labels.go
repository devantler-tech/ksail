package v1alpha1

// ManagedNamespaceLabel marks a Namespace that the KSail operator created on demand to hold a
// Cluster resource. The operator only ever deletes namespaces carrying this label, so namespaces
// that already existed (e.g. "default" or user-managed namespaces) are never removed.
const ManagedNamespaceLabel = "ksail.io/managed-namespace"
