package tenant

import "errors"

var (
	// ErrInvalidType is returned when an invalid tenant type is provided.
	ErrInvalidType = errors.New("invalid tenant type")
	// ErrTenantNameRequired is returned when the tenant name is empty.
	ErrTenantNameRequired = errors.New("tenant name is required")
	// ErrTenantTypeRequired is returned when the tenant type is not specified and cannot be auto-detected.
	ErrTenantTypeRequired = errors.New(
		"tenant type is required (use --type flag or configure gitOpsEngine in ksail.yaml)",
	)
	// ErrKustomizationNotFound is returned when no kustomization.yaml is found.
	ErrKustomizationNotFound = errors.New("kustomization.yaml not found")
	// ErrKustomizationIsDirectory is returned when the kustomization path points to a directory.
	ErrKustomizationIsDirectory = errors.New("kustomization path is a directory, not a file")
	// ErrInvalidTenantName is returned when the tenant name is not a valid DNS-1123 label.
	ErrInvalidTenantName = errors.New("invalid tenant name")
	// ErrInvalidNamespace is returned when a namespace is not a valid DNS-1123 label.
	ErrInvalidNamespace = errors.New("invalid namespace")
	// ErrDuplicateNamespace is returned when duplicate namespaces are provided.
	ErrDuplicateNamespace = errors.New("duplicate namespaces are not allowed")
	// ErrNamespaceRequired is returned when no namespace is provided.
	ErrNamespaceRequired = errors.New("at least one namespace is required")
	// ErrGitProviderRequired is returned when --git-provider is required but not set.
	ErrGitProviderRequired = errors.New("--git-provider is required")
	// ErrTenantRepoRequired is returned when --tenant-repo is required but not set.
	ErrTenantRepoRequired = errors.New("--tenant-repo is required")
	// ErrRegistryRequired is returned when --registry is required but not set.
	ErrRegistryRequired = errors.New("--registry is required for Flux OCI sync source")
	// ErrUnsupportedSyncSource is returned when an unsupported sync source is specified.
	ErrUnsupportedSyncSource = errors.New("unsupported sync source")
	// ErrTenantAlreadyExists is returned when a tenant directory already exists.
	ErrTenantAlreadyExists = errors.New("tenant directory already exists")
	// ErrTenantDirNotExist is returned when a tenant directory does not exist.
	ErrTenantDirNotExist = errors.New("tenant directory does not exist")
	// ErrOutsideKustomizationRoot is returned when a tenant path is outside the kustomization root.
	ErrOutsideKustomizationRoot = errors.New("tenant directory is outside the kustomization root")
	// ErrInvalidDelivery is returned when an invalid --delivery value is provided.
	ErrInvalidDelivery = errors.New("invalid --delivery value")
	// ErrInvalidSyncSource is returned when an invalid --sync-source value is provided.
	ErrInvalidSyncSource = errors.New("invalid --sync-source value")
	// ErrConfigNotFound is returned when no --type is specified and no ksail.yaml is found.
	ErrConfigNotFound = errors.New(
		"no --type specified and no ksail.yaml found: " +
			"please specify --type (flux, argocd, or kubectl)",
	)
	// ErrDeleteRepoGitProviderRequired is returned when --git-provider is required with --delete-repo.
	ErrDeleteRepoGitProviderRequired = errors.New(
		"--git-provider is required when --delete-repo is set",
	)
	// ErrDeleteRepoTenantRepoRequired is returned when --tenant-repo is required with --delete-repo.
	ErrDeleteRepoTenantRepoRequired = errors.New("--tenant-repo is required when --delete-repo is set")
	// ErrPlatformRepoRequired is returned when the platform repo cannot be resolved.
	ErrPlatformRepoRequired = errors.New("--platform-repo is required (or run from a git repo with a remote)")
	// ErrRBACConfigMapNotFound is returned when no argocd-rbac-cm ConfigMap file is found.
	ErrRBACConfigMapNotFound = errors.New("no argocd-rbac-cm ConfigMap found")
)
