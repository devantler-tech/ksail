package argocd

import "context"

// Installer installs or ensures Argo CD is present in the cluster.
//
// Implementations are expected to be idempotent.
type Installer interface {
	Install(ctx context.Context) error
}

// Manager ensures Argo CD GitOps resources exist and can update reconciliation
// to a new OCI revision.
//
// Implementations are expected to be idempotent.
type Manager interface {
	Ensure(ctx context.Context, opts EnsureOptions) error
	UpdateTargetRevision(ctx context.Context, opts UpdateTargetRevisionOptions) error
}

// StatusProvider returns a lightweight, user-facing status summary.
type StatusProvider interface {
	GetStatus(ctx context.Context) (Status, error)
}

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

// Status is a lightweight user-facing summary of Argo CD state.
type Status struct {
	// Engine is a human-friendly engine name, e.g. "ArgoCD".
	Engine string
	// Installed indicates whether Argo CD appears installed.
	Installed bool
	// ApplicationPresent indicates whether the expected Application exists.
	ApplicationPresent bool
	// Message is a short user-facing summary.
	Message string
}
