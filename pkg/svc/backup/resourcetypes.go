package backup

// Kubernetes resource type names (kubectl plural forms) backed up and restored
// by the engine. They are named constants so the ordered backup list and the
// cluster-scoped lookup are single-sourced and free of duplicated string
// literals.
const (
	rtCRDs                   = "customresourcedefinitions"
	rtNamespaces             = "namespaces"
	rtStorageClasses         = "storageclasses"
	rtPersistentVolumes      = "persistentvolumes"
	rtPersistentVolumeClaims = "persistentvolumeclaims"
	rtSecrets                = "secrets"
	rtConfigMaps             = "configmaps"
	rtServiceAccounts        = "serviceaccounts"
	rtRoles                  = "roles"
	rtRoleBindings           = "rolebindings"
	rtClusterRoles           = "clusterroles"
	rtClusterRoleBindings    = "clusterrolebindings"
	rtServices               = "services"
	rtDeployments            = "deployments"
	rtStatefulSets           = "statefulsets"
	rtDaemonSets             = "daemonsets"
	rtJobs                   = "jobs"
	rtCronJobs               = "cronjobs"
	rtIngresses              = "ingresses"
)

// clusterScopedResourceTypes returns resource types that are cluster-scoped
// (not namespaced). These should never use -n or --all-namespaces flags.
func clusterScopedResourceTypes() map[string]bool {
	return map[string]bool{
		rtCRDs:                true,
		rtNamespaces:          true,
		rtStorageClasses:      true,
		rtPersistentVolumes:   true,
		rtClusterRoles:        true,
		rtClusterRoleBindings: true,
	}
}

// backupResourceTypes returns the ordered list of resource types for backup.
// CRDs and cluster-scoped resources come first, followed by storage, RBAC,
// and workloads. Restore replays this same ordering.
func backupResourceTypes() []string {
	return []string{
		rtCRDs,
		rtNamespaces,
		rtStorageClasses,
		rtPersistentVolumes,
		rtPersistentVolumeClaims,
		rtSecrets,
		rtConfigMaps,
		rtServiceAccounts,
		rtRoles,
		rtRoleBindings,
		rtClusterRoles,
		rtClusterRoleBindings,
		rtServices,
		rtDeployments,
		rtStatefulSets,
		rtDaemonSets,
		rtJobs,
		rtCronJobs,
		rtIngresses,
	}
}
