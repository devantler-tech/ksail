package helm

// RepoConfig holds repository configuration for a Helm chart.
type RepoConfig struct {
	// Name is the repository identifier used in Helm commands.
	Name string
	// URL is the Helm repository URL.
	URL string
	// RepoName is the human-readable name used in error messages.
	RepoName string
}

// ChartConfig holds chart installation configuration.
type ChartConfig struct {
	// ReleaseName is the Helm release name.
	ReleaseName string
	// ChartName is the chart identifier (e.g., "repo/chart").
	ChartName string
	// Namespace is the Kubernetes namespace for installation.
	Namespace string
	// RepoURL is the Helm repository URL.
	RepoURL string
	// CreateNamespace determines if the namespace should be created.
	CreateNamespace bool
	// SetJSONVals contains JSON values to set during installation.
	SetJSONVals map[string]string
}
