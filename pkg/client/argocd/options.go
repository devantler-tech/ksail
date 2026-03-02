package argocd

// EnsureOptions configures how KSail ensures Argo CD resources.
type EnsureOptions struct {
	// RepositoryURL is the Argo CD repository URL, typically an OCI repository.
	// Example: oci://local-registry:5000/<repository>
	RepositoryURL string

	// SourcePath is the path inside the repository to the kustomization root.
	// This is derived from the existing `spec.sourceDirectory` setting.
	//
	// If empty, defaults to "k8s".
	SourcePath string

	// ApplicationName is the Argo CD Application name.
	ApplicationName string

	// TargetRevision is the initial revision (tag or digest).
	TargetRevision string

	// Username for OCI registry authentication (optional, for external registries).
	Username string

	// Password for OCI registry authentication (optional, for external registries).
	Password string

	// Insecure allows HTTP connections (for local registries). Default is false.
	Insecure bool
}

// UpdateTargetRevisionOptions configures how KSail updates an Application to a new OCI revision.
type UpdateTargetRevisionOptions struct {
	ApplicationName string
	TargetRevision  string
	// HardRefresh requests Argo CD to refresh caches when updating revision.
	HardRefresh bool
}
