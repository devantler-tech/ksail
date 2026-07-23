package helm

import (
	"context"
	"errors"
	"time"

	helmv4driver "helm.sh/helm/v4/pkg/storage/driver"
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
	errGetReleaseValuesUnsupported = errors.New(
		"helm: GetReleaseValues not supported on template-only client",
	)

	// ErrNoReleaseStorage is returned by GetReleaseStorageLabels when no
	// Helm release storage objects (Secrets or ConfigMaps) exist for the
	// given release name and namespace.
	ErrNoReleaseStorage = errors.New("helm: no release storage objects found")
	// ErrReleaseStorageDriverUnsupported reports a Helm backend that cannot
	// provide Kubernetes object identity for fail-closed release ownership.
	ErrReleaseStorageDriverUnsupported = errors.New(
		"helm storage driver cannot provide a Kubernetes release identity",
	)

	// ErrReleaseNotFound re-exports the Helm SDK sentinel so callers can
	// check whether an uninstall failed because the release never existed
	// without importing the Helm driver package directly.
	ErrReleaseNotFound = helmv4driver.ErrReleaseNotFound
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

// ReleaseStorageMetadata identifies one concrete Helm storage object. Identity
// is the Kubernetes object UID, so deleting and reinstalling a same-name
// release produces different ownership evidence even when its revision resets.
type ReleaseStorageMetadata struct {
	Labels            map[string]string
	Identity          string
	HistoryIdentities []string
}

// ChartManager defines the chart lifecycle operations required by KSail.
type ChartManager interface {
	InstallChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error)
	InstallOrUpgradeChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error)
	UninstallRelease(ctx context.Context, releaseName, namespace string) error
	AddRepository(ctx context.Context, entry *RepositoryEntry, timeout time.Duration) error
	TemplateChart(ctx context.Context, spec *ChartSpec) (string, error)
}

// ReleaseInspector defines the live release queries required by KSail.
type ReleaseInspector interface {
	ReleaseExists(ctx context.Context, releaseName, namespace string) (bool, error)
	// ListReleases returns Helm releases across all namespaces for all statuses.
	// Name, Namespace, Revision, and Status are populated; the remaining fields
	// are left at their zero values. Use this for bulk release detection to avoid
	// N separate ReleaseExists roundtrips.
	ListReleases(ctx context.Context) ([]ReleaseInfo, error)
	// GetReleaseStorageLabels returns the Kubernetes object labels from the
	// latest Helm release storage object (Secret or ConfigMap, depending on
	// HELM_DRIVER) for the given release name and namespace. Returns
	// (nil, ErrNoReleaseStorage) when no matching objects exist. This is
	// used to inspect labels added by external controllers (e.g. Flux
	// helm-controller) to determine ownership.
	GetReleaseStorageLabels(
		ctx context.Context,
		releaseName, namespace string,
	) (map[string]string, error)
	// GetReleaseStorageMetadata returns labels and the Kubernetes UID from the
	// latest Helm release storage object. The UID binds persisted ownership to
	// one release incarnation rather than only to its reusable Helm name.
	GetReleaseStorageMetadata(
		ctx context.Context,
		releaseName, namespace string,
	) (*ReleaseStorageMetadata, error)
	// GetReleaseValues returns the user-supplied values for the latest revision
	// of the named release. Returns (nil, error) when the release does not exist
	// or cannot be queried. Use this to introspect installed chart configuration
	// (e.g., detecting autoscaler settings from the live cluster).
	GetReleaseValues(
		ctx context.Context,
		releaseName, namespace string,
	) (map[string]any, error)
	// RefreshDiscovery invalidates cached Kubernetes API discovery so subsequent
	// operations observe CRDs (and other API resources) registered since the
	// client was created. Helm caches discovery both on disk and in an in-memory
	// RESTMapper; when CRDs are installed by one release and the custom resources
	// that depend on them by another, the second install fails with "ensure CRDs
	// are installed first" unless discovery is refreshed in between.
	RefreshDiscovery() error
}

// Interface defines the subset of Helm functionality required by KSail.
type Interface interface {
	ChartManager
	ReleaseInspector
}
