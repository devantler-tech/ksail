package helm

import (
	"time"

	helmv4action "helm.sh/helm/v4/pkg/action"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	helmv4kube "helm.sh/helm/v4/pkg/kube"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// Expose unexported functions for testing.
//
//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
var (
	ParseChartRef            = parseChartRef
	BuildChartPathOptions    = buildChartPathOptions
	ApplyChartPathOptions    = applyChartPathOptions
	MergeSetValues           = mergeSetValues
	MergeSetJSONValues       = mergeSetJSONValues
	MergeValuesYaml          = mergeValuesYaml
	MergeMapsInto            = mergeMapsInto
	ReleaseToInfo            = releaseToInfo
	ExecuteAndExtractRelease = executeAndExtractRelease
	ApplyCommonActionConfig  = applyCommonActionConfig
)

// Expose unexported error sentinels and repository helpers for test assertions.
var (
	ErrUnexpectedReleaseType   = errUnexpectedReleaseType
	ErrReleaseNameRequired     = errReleaseNameRequired
	ErrChartSpecRequired       = errChartSpecRequired
	ErrRepositoryEntryRequired = errRepositoryEntryRequired
	ErrRepositoryNameRequired  = errRepositoryNameRequired
	ErrRepositoryCacheUnset    = errRepositoryCacheUnset
	ErrRepositoryConfigUnset   = errRepositoryConfigUnset
	ErrListReleasesUnsupported = errListReleasesUnsupported
	//nolint:gochecknoglobals // export_test.go exposes package internals as globals for tests.
	ConvertRepositoryEntry = convertRepositoryEntry
	//nolint:gochecknoglobals // export_test.go exposes package internals as globals for tests.
	LoadOrInitRepositoryFile = loadOrInitRepositoryFile
	//nolint:gochecknoglobals // export_test.go exposes package internals as globals for tests.
	ValidateRepositoryRequest = validateRepositoryRequest
)

// TestableActionConfig captures calls to actionConfig for verification in tests.
type TestableActionConfig struct {
	WaitStrategy helmv4kube.WaitStrategy
	WaitForJobs  bool
	Timeout      time.Duration
	Version      string
}

func (t *TestableActionConfig) setWaitStrategy(s helmv4kube.WaitStrategy) { t.WaitStrategy = s }
func (t *TestableActionConfig) setWaitForJobs(w bool)                     { t.WaitForJobs = w }
func (t *TestableActionConfig) setTimeout(d time.Duration)                { t.Timeout = d }
func (t *TestableActionConfig) setVersion(v string)                       { t.Version = v }

// NewInstallActionAdapter creates an installActionAdapter wrapping an Install action.
func NewInstallActionAdapter() (actionConfig, *helmv4action.Install) {
	install := &helmv4action.Install{}

	return installActionAdapter{install}, install
}

// NewUpgradeActionAdapter creates an upgradeActionAdapter wrapping an Upgrade action.
func NewUpgradeActionAdapter() (actionConfig, *helmv4action.Upgrade) {
	upgrade := &helmv4action.Upgrade{}

	return upgradeActionAdapter{upgrade}, upgrade
}

// NewInstallAction creates an Install action for testing applyChartPathOptions.
func NewInstallAction() *helmv4action.Install {
	return &helmv4action.Install{}
}

// NewUpgradeAction creates an Upgrade action for testing applyChartPathOptions.
func NewUpgradeAction() *helmv4action.Upgrade {
	return &helmv4action.Upgrade{}
}

// NewTestRelease creates a v1.Release for testing releaseToInfo.
func NewTestRelease(
	name, namespace, chartName, appVersion, notes string,
	status releasecommon.Status,
	version int,
	lastDeployed time.Time,
) *v1.Release {
	return &v1.Release{
		Name:      name,
		Namespace: namespace,
		Version:   version,
		Info: &v1.Info{
			Status:       status,
			LastDeployed: lastDeployed,
			Notes:        notes,
		},
		Chart: &chartv2.Chart{
			Metadata: &chartv2.Metadata{
				Name:       chartName,
				AppVersion: appVersion,
			},
		},
	}
}
