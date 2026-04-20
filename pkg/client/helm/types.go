package helm

import (
	"context"
	"errors"
	"time"
)

var (
	errReleaseNameRequired     = errors.New("helm: release name is required")
	errChartSpecRequired       = errors.New("helm: chart spec is required")
	errUnexpectedReleaseType   = errors.New("helm: unexpected release type")
	errUnexpectedChartType     = errors.New("helm: unexpected chart type")
	errUnsupportedClientType   = errors.New("helm: unsupported client type for OCI chart")
	errListReleasesUnsupported = errors.New(
		"helm: ListReleases not supported on template-only client",
	)
)

// ChartSpec mirrors the mittwald chart specification while keeping KSail
// specific convenience fields.
type ChartSpec struct {
	ReleaseName string
	ChartName   string
	Namespace   string
	Version     string

	CreateNamespace bool
	Atomic          bool
	// Wait enables kstatus-based waiting for resources to be ready (HIP-0022).
	// When true, Helm uses StatusWatcherStrategy which supports custom resources
	// and ensures full reconciliation of all resources.
	Wait bool
	// WaitForJobs extends Wait to also wait for Job completion.
	WaitForJobs bool
	Timeout     time.Duration
	Silent      bool
	UpgradeCRDs bool

	ValuesYaml  string
	ValueFiles  []string
	SetValues   map[string]string
	SetFileVals map[string]string
	SetJSONVals map[string]string

	RepoURL  string
	Username string

	Password              string
	CertFile              string
	KeyFile               string
	CaFile                string
	InsecureSkipTLSverify bool
}

// RepositoryEntry describes a Helm repository that should be added locally
// before performing chart operations.
type RepositoryEntry struct {
	Name     string
	URL      string
	Username string

	Password              string
	CertFile              string
	KeyFile               string
	CaFile                string
	InsecureSkipTLSverify bool
	PlainHTTP             bool
}

// ReleaseInfo captures metadata about a Helm release after an operation.
type ReleaseInfo struct {
	Name       string
	Namespace  string
	Revision   int
	Status     string
	Chart      string
	AppVersion string
	Updated    time.Time
	Notes      string
}

// Interface defines the subset of Helm functionality required by KSail.
type Interface interface {
	InstallChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error)
	InstallOrUpgradeChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error)
	UninstallRelease(ctx context.Context, releaseName, namespace string) error
	AddRepository(ctx context.Context, entry *RepositoryEntry, timeout time.Duration) error
	TemplateChart(ctx context.Context, spec *ChartSpec) (string, error)
	ReleaseExists(ctx context.Context, releaseName, namespace string) (bool, error)
	// ListReleases returns Helm releases across all namespaces for all statuses.
	// Only the Name and Namespace fields of each ReleaseInfo are guaranteed to be
	// populated; all other fields (Status, Revision, etc.) are left at their zero
	// values. Use this for bulk release detection to avoid N separate ReleaseExists roundtrips.
	ListReleases(ctx context.Context) ([]ReleaseInfo, error)
}
