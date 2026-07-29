package argocd

// EnsureOptions configures how KSail ensures Argo CD resources.
type EnsureOptions struct {
	// RepositoryURL is the Argo CD repository URL, typically an OCI repository.
	// Example: oci://local-registry:5000/<repository>
	RepositoryURL string

	// SourcePath is the path inside the OCI artifact to the kustomization root,
	// resolved by Argo CD relative to the root of the expanded archive.
	//
	// If empty, defaults to DefaultSourcePath ("."). The workload artifact
	// builder publishes manifests at the archive root, so the default selects
	// the contents of `spec.sourceDirectory` directly.
	SourcePath string

	// ApplicationName is the Argo CD Application name.
	ApplicationName string

	// TargetRevision is the initial revision (tag or digest).
	TargetRevision string

	// Username for OCI registry authentication (optional, for external registries).
	Username string

	// Password for OCI registry authentication (optional, for external registries).
	Password string
	// PullOnlyCredentials marks Username and Password as cluster-resident pull
	// credentials so push auto-detection cannot reuse them.
	PullOnlyCredentials bool

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
