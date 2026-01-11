package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	ksailio "github.com/devantler-tech/ksail/v5/pkg/io"
	helmv4action "helm.sh/helm/v4/pkg/action"
	helmv4loader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	helmv4cli "helm.sh/helm/v4/pkg/cli"
	helmv4downloader "helm.sh/helm/v4/pkg/downloader"
	helmv4getter "helm.sh/helm/v4/pkg/getter"
	helmv4kube "helm.sh/helm/v4/pkg/kube"
	helmv4registry "helm.sh/helm/v4/pkg/registry"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	repov1 "helm.sh/helm/v4/pkg/repo/v1"
	helmv4strvals "helm.sh/helm/v4/pkg/strvals"
	"sigs.k8s.io/yaml"
)

const (
	// DefaultTimeout defines the fallback Helm chart installation timeout.
	DefaultTimeout = 5 * time.Minute
	repoDirMode    = 0o750
	repoFileMode   = 0o640
	chartRefParts  = 2
)

var (
	errReleaseNameRequired     = errors.New("helm: release name is required")
	errRepositoryEntryRequired = errors.New("helm: repository entry is required")
	errRepositoryNameRequired  = errors.New("helm: repository name is required")
	errRepositoryCacheUnset    = errors.New("helm: repository cache path is not set")
	errRepositoryConfigUnset   = errors.New("helm: repository config path is not set")
	errChartSpecRequired       = errors.New("helm: chart spec is required")
	errUnexpectedReleaseType   = errors.New("helm: unexpected release type")
	errUnexpectedChartType     = errors.New("helm: unexpected chart type")
	errUnsupportedClientType   = errors.New("helm: unsupported client type for OCI chart")
)

// stderrCaptureMu protects process-wide stderr redirection from concurrent access.
var stderrCaptureMu sync.Mutex //nolint:gochecknoglobals // global lock required to coordinate stderr interception

// helmActionConfig defines common configuration fields shared by Install and Upgrade actions.
type helmActionConfig interface {
	setWaitStrategy(strategy helmv4kube.WaitStrategy)
	setWaitForJobs(wait bool)
	setTimeout(timeout time.Duration)
	setVersion(version string)
}

// installActionAdapter wraps Install to implement helmActionConfig.
type installActionAdapter struct{ *helmv4action.Install }

func (a installActionAdapter) setWaitStrategy(s helmv4kube.WaitStrategy) { a.WaitStrategy = s }
func (a installActionAdapter) setWaitForJobs(w bool)                     { a.WaitForJobs = w }
func (a installActionAdapter) setTimeout(t time.Duration)                { a.Timeout = t }
func (a installActionAdapter) setVersion(v string)                       { a.Version = v }

// upgradeActionAdapter wraps Upgrade to implement helmActionConfig.
type upgradeActionAdapter struct{ *helmv4action.Upgrade }

func (a upgradeActionAdapter) setWaitStrategy(s helmv4kube.WaitStrategy) { a.WaitStrategy = s }
func (a upgradeActionAdapter) setWaitForJobs(w bool)                     { a.WaitForJobs = w }
func (a upgradeActionAdapter) setTimeout(t time.Duration)                { a.Timeout = t }
func (a upgradeActionAdapter) setVersion(v string)                       { a.Version = v }

// applyCommonActionConfig applies shared configuration from spec to action.
func applyCommonActionConfig(action helmActionConfig, spec *ChartSpec) {
	if spec.Wait {
		action.setWaitStrategy(helmv4kube.StatusWatcherStrategy)
	} else {
		action.setWaitStrategy(helmv4kube.HookOnlyStrategy)
	}

	action.setWaitForJobs(spec.WaitForJobs)

	timeout := spec.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	action.setTimeout(timeout)
	action.setVersion(spec.Version)
}

// ChartSpec mirrors the mittwald chart specification while keeping KSail
// specific convenience fields.
type ChartSpec struct {
	ReleaseName string
	ChartName   string
	Namespace   string
	Version     string

	CreateNamespace bool
	Atomic          bool
	Wait            bool
	WaitForJobs     bool
	Timeout         time.Duration
	Silent          bool
	UpgradeCRDs     bool

	ValuesYaml  string
	ValueFiles  []string
	SetValues   map[string]string
	SetFileVals map[string]string
	SetJSONVals map[string]string

	RepoURL               string
	Username              string
	Password              string
	CertFile              string
	KeyFile               string
	CaFile                string
	InsecureSkipTLSverify bool
}

// RepositoryEntry describes a Helm repository that should be added locally
// before performing chart operations.
type RepositoryEntry struct {
	Name                  string
	URL                   string
	Username              string
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
//
//go:generate mockery --name=Interface --output=. --filename=mocks.go
type Interface interface {
	InstallChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error)
	InstallOrUpgradeChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error)
	UninstallRelease(ctx context.Context, releaseName, namespace string) error
	AddRepository(ctx context.Context, entry *RepositoryEntry, timeout time.Duration) error
}

// Client represents the default helm implementation used by KSail.
type Client struct {
	actionConfig *helmv4action.Configuration
	settings     *helmv4cli.EnvSettings
	kubeConfig   string
	kubeContext  string
	debugLog     func(string, ...any)
}

var _ Interface = (*Client)(nil)

// NewClient creates a Helm client using the provided kubeconfig and context.
func NewClient(kubeConfig, kubeContext string) (*Client, error) {
	return newClient(kubeConfig, kubeContext, nil)
}

func newClient(
	kubeConfig, kubeContext string,
	debug func(string, ...any),
) (*Client, error) {
	// Initialize Helm v4 settings and action configuration
	debugLog := debug
	if debugLog == nil {
		debugLog = func(string, ...any) {}
	}

	settings := helmv4cli.New()

	if kubeConfig != "" {
		settings.KubeConfig = kubeConfig
	}

	if kubeContext != "" {
		settings.KubeContext = kubeContext
	}

	actionConfig := new(helmv4action.Configuration)

	initErr := actionConfig.Init(
		settings.RESTClientGetter(),
		settings.Namespace(),
		os.Getenv("HELM_DRIVER"),
	)
	if initErr != nil {
		return nil, fmt.Errorf("failed to initialize helm v4 action config: %w", initErr)
	}

	return &Client{
		actionConfig: actionConfig,
		settings:     settings,
		kubeConfig:   kubeConfig,
		kubeContext:  kubeContext,
		debugLog:     debugLog,
	}, nil
}

// InstallChart installs a Helm chart using the provided specification.
func (c *Client) InstallChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error) {
	return c.installRelease(ctx, spec, false)
}

// InstallOrUpgradeChart upgrades a Helm chart when present and installs it otherwise.
func (c *Client) InstallOrUpgradeChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error) {
	return c.installRelease(ctx, spec, true)
}

// UninstallRelease removes a Helm release by name within the provided namespace.
func (c *Client) UninstallRelease(ctx context.Context, releaseName, namespace string) error {
	if releaseName == "" {
		return errReleaseNameRequired
	}

	ctxErr := ctx.Err()
	if ctxErr != nil {
		return fmt.Errorf("uninstall release context cancelled: %w", ctxErr)
	}

	cleanup, err := c.switchNamespace(namespace)
	if err != nil {
		return err
	}

	defer cleanup()

	client := helmv4action.NewUninstall(c.actionConfig)
	client.KeepHistory = false

	_, uninstallErr := client.Run(releaseName)
	if uninstallErr != nil {
		return fmt.Errorf("uninstall release %q: %w", releaseName, uninstallErr)
	}

	return nil
}

// AddRepository registers a Helm repository for the current client instance.
// The timeout parameter controls how long HTTP requests for downloading the repository index can take.
func (c *Client) AddRepository(
	ctx context.Context,
	entry *RepositoryEntry,
	timeout time.Duration,
) error {
	requestErr := validateRepositoryRequest(ctx, entry)
	if requestErr != nil {
		return requestErr
	}

	settings := c.settings

	repoFile, err := ensureRepositoryConfig(settings)
	if err != nil {
		return err
	}

	repositoryFile := loadOrInitRepositoryFile(repoFile)
	repoEntry := convertRepositoryEntry(entry)

	repoCache, err := ensureRepositoryCache(settings)
	if err != nil {
		return err
	}

	chartRepository, err := newChartRepository(settings, repoEntry, repoCache, timeout)
	if err != nil {
		return err
	}

	downloadErr := downloadRepositoryIndex(chartRepository)
	if downloadErr != nil {
		return downloadErr
	}

	repositoryFile.Update(repoEntry)

	writeErr := repositoryFile.WriteFile(repoFile, repoFileMode)
	if writeErr != nil {
		return fmt.Errorf("write repository file: %w", writeErr)
	}

	return nil
}

func validateRepositoryRequest(ctx context.Context, entry *RepositoryEntry) error {
	if entry == nil {
		return errRepositoryEntryRequired
	}

	if entry.Name == "" {
		return errRepositoryNameRequired
	}

	ctxErr := ctx.Err()
	if ctxErr != nil {
		return fmt.Errorf("add repository context cancelled: %w", ctxErr)
	}

	return nil
}

func ensureRepositoryConfig(settings *helmv4cli.EnvSettings) (string, error) {
	repoFile := settings.RepositoryConfig

	envRepoConfig := os.Getenv("HELM_REPOSITORY_CONFIG")
	if envRepoConfig != "" {
		repoFile = envRepoConfig
		settings.RepositoryConfig = envRepoConfig
	}

	if repoFile == "" {
		return "", errRepositoryConfigUnset
	}

	repoDir := filepath.Dir(repoFile)

	mkdirErr := os.MkdirAll(repoDir, repoDirMode)
	if mkdirErr != nil {
		return "", fmt.Errorf("create repository directory: %w", mkdirErr)
	}

	return repoFile, nil
}

func loadOrInitRepositoryFile(repoFile string) *repov1.File {
	repositoryFile, err := repov1.LoadFile(repoFile)
	if err != nil {
		return repov1.NewFile()
	}

	return repositoryFile
}

func convertRepositoryEntry(entry *RepositoryEntry) *repov1.Entry {
	return &repov1.Entry{
		Name:                  entry.Name,
		URL:                   entry.URL,
		Username:              entry.Username,
		Password:              entry.Password,
		CertFile:              entry.CertFile,
		KeyFile:               entry.KeyFile,
		CAFile:                entry.CaFile,
		InsecureSkipTLSverify: entry.InsecureSkipTLSverify,
	}
}

func ensureRepositoryCache(settings *helmv4cli.EnvSettings) (string, error) {
	repoCache := settings.RepositoryCache

	if envCache := os.Getenv("HELM_REPOSITORY_CACHE"); envCache != "" {
		repoCache = envCache
		settings.RepositoryCache = envCache
	}

	if repoCache == "" {
		return "", errRepositoryCacheUnset
	}

	mkdirCacheErr := os.MkdirAll(repoCache, repoDirMode)
	if mkdirCacheErr != nil {
		return "", fmt.Errorf("create repository cache directory: %w", mkdirCacheErr)
	}

	return repoCache, nil
}

func newChartRepository(
	settings *helmv4cli.EnvSettings,
	repoEntry *repov1.Entry,
	repoCache string,
	timeout time.Duration,
) (*repov1.ChartRepository, error) {
	// Use getter.WithTimeout to configure HTTP timeout for repository index downloads.
	// This prevents hangs when the repository server is slow to respond.
	getterOpts := []helmv4getter.Option{}
	if timeout > 0 {
		getterOpts = append(getterOpts, helmv4getter.WithTimeout(timeout))
	}

	chartRepository, err := repov1.NewChartRepository(
		repoEntry,
		helmv4getter.All(settings, getterOpts...),
	)
	if err != nil {
		return nil, fmt.Errorf("create chart repository: %w", err)
	}

	chartRepository.CachePath = repoCache

	return chartRepository, nil
}

func downloadRepositoryIndex(chartRepository *repov1.ChartRepository) error {
	indexPath, err := chartRepository.DownloadIndexFile()
	if err != nil {
		return fmt.Errorf("failed to download repository index file: %w", err)
	}

	_, statErr := os.Stat(indexPath)
	if statErr != nil {
		return fmt.Errorf("failed to verify repository index file: %w", statErr)
	}

	return nil
}

func (c *Client) installRelease(
	ctx context.Context,
	spec *ChartSpec,
	upgrade bool,
) (*ReleaseInfo, error) {
	if spec == nil {
		return nil, errChartSpecRequired
	}

	cleanup, err := c.switchNamespace(spec.Namespace)
	if err != nil {
		return nil, err
	}

	defer cleanup()

	// Check if release exists when doing upgrade
	var rel *v1.Release

	if upgrade {
		histClient := helmv4action.NewHistory(c.actionConfig)
		histClient.Max = 1
		releases, histErr := histClient.Run(spec.ReleaseName)

		if histErr == nil && len(releases) > 0 {
			// Release exists, perform upgrade
			rel, err = c.upgradeRelease(ctx, spec)
		} else {
			// Release doesn't exist, perform install
			rel, err = c.performInstall(ctx, spec)
		}
	} else {
		rel, err = c.performInstall(ctx, spec)
	}

	if err != nil {
		return nil, err
	}

	return releaseToInfo(rel), nil
}

func (c *Client) performInstall(ctx context.Context, spec *ChartSpec) (*v1.Release, error) {
	client := helmv4action.NewInstall(c.actionConfig)
	client.ReleaseName = spec.ReleaseName
	client.Namespace = spec.Namespace
	client.CreateNamespace = spec.CreateNamespace

	applyCommonActionConfig(installActionAdapter{client}, spec)

	// Note: Atomic is not supported in Helm v4 Install action

	chart, vals, err := c.loadChartAndValues(spec, client)
	if err != nil {
		return nil, err
	}

	runFn := func() (any, error) {
		return client.RunWithContext(ctx, chart, vals)
	}

	return executeAndExtractRelease(runFn, spec.Silent)
}

func (c *Client) upgradeRelease(ctx context.Context, spec *ChartSpec) (*v1.Release, error) {
	client := helmv4action.NewUpgrade(c.actionConfig)
	client.Namespace = spec.Namespace

	applyCommonActionConfig(upgradeActionAdapter{client}, spec)

	// Note: Atomic is not supported in Helm v4 Upgrade action
	client.SkipCRDs = !spec.UpgradeCRDs // Inverted logic in v4

	chart, vals, err := c.loadChartAndValues(spec, client)
	if err != nil {
		return nil, err
	}

	runFn := func() (any, error) {
		return client.RunWithContext(ctx, spec.ReleaseName, chart, vals)
	}

	return executeAndExtractRelease(runFn, spec.Silent)
}

func (c *Client) loadChartAndValues(
	spec *ChartSpec,
	client any,
) (*chartv2.Chart, map[string]any, error) {
	chartPath, chart, err := c.locateAndLoadChart(spec, client)
	if err != nil {
		return nil, nil, err
	}

	vals, err := c.mergeValues(spec, chartPath)
	if err != nil {
		return nil, nil, err
	}

	return chart, vals, nil
}

func executeAndExtractRelease(
	runFn func() (any, error),
	silent bool,
) (*v1.Release, error) {
	var releaser any

	var err error

	if silent {
		releaser, err = runWithSilencedStderr(runFn)
	} else {
		releaser, err = runFn()
	}

	if err != nil {
		return nil, err
	}

	rel, ok := releaser.(*v1.Release)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errUnexpectedReleaseType, releaser)
	}

	return rel, nil
}

func (c *Client) locateAndLoadChart(
	spec *ChartSpec,
	client any,
) (string, *chartv2.Chart, error) {
	var chartPath string

	var err error

	switch {
	case spec.RepoURL != "":
		chartPath, err = c.locateChartFromRepo(spec, client)
	case helmv4registry.IsOCI(spec.ChartName):
		chartPath, err = c.locateOCIChart(spec, client)
	default:
		chartPath = spec.ChartName
	}

	if err != nil {
		return "", nil, err
	}

	chartInterface, err := helmv4loader.Load(chartPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// Type assert to *chartv2.Chart
	chart, ok := chartInterface.(*chartv2.Chart)
	if !ok {
		return "", nil, fmt.Errorf("%w: %T", errUnexpectedChartType, chartInterface)
	}

	return chartPath, chart, nil
}

// buildChartPathOptions creates common ChartPathOptions from a ChartSpec.
func buildChartPathOptions(spec *ChartSpec, repoURL string) helmv4action.ChartPathOptions {
	return helmv4action.ChartPathOptions{
		RepoURL:               repoURL,
		Version:               spec.Version,
		Username:              spec.Username,
		Password:              spec.Password,
		CertFile:              spec.CertFile,
		KeyFile:               spec.KeyFile,
		CaFile:                spec.CaFile,
		InsecureSkipTLSverify: spec.InsecureSkipTLSverify,
	}
}

func (c *Client) locateOCIChart(spec *ChartSpec, client any) (string, error) {
	// Create a registry client for OCI operations
	registryClient, err := helmv4registry.NewClient()
	if err != nil {
		return "", fmt.Errorf("failed to create registry client: %w", err)
	}

	opts := buildChartPathOptions(spec, "")

	// Apply options and registry client, then locate chart
	var chartPath string

	switch actionClient := client.(type) {
	case *helmv4action.Install:
		actionClient.ChartPathOptions = opts
		actionClient.SetRegistryClient(registryClient)
		chartPath, err = actionClient.LocateChart(spec.ChartName, c.settings)
	case *helmv4action.Upgrade:
		actionClient.ChartPathOptions = opts
		actionClient.SetRegistryClient(registryClient)
		chartPath, err = actionClient.LocateChart(spec.ChartName, c.settings)
	default:
		return "", fmt.Errorf("%w: %T", errUnsupportedClientType, client)
	}

	if err != nil {
		return "", fmt.Errorf("failed to locate OCI chart %q: %w", spec.ChartName, err)
	}

	return chartPath, nil
}

func (c *Client) locateChartFromRepo(spec *ChartSpec, _ any) (string, error) {
	_, chartName := parseChartRef(spec.ChartName)
	if chartName == "" {
		chartName = spec.ChartName
	}

	// Use custom chart downloader with configurable timeout instead of LocateChart
	// which has a hardcoded 120-second HTTP timeout that can cause failures when
	// downloading large charts (like Calico/Tigera).
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	chartPath, err := c.downloadChartWithTimeout(spec, chartName, timeout)
	if err != nil {
		return "", fmt.Errorf(
			"failed to locate chart %q in repository %s: %w",
			chartName,
			spec.RepoURL,
			err,
		)
	}

	return chartPath, nil
}

// downloadChartWithTimeout downloads a chart using a custom ChartDownloader with the specified timeout.
// This bypasses Helm's LocateChart which has a hardcoded 120-second HTTP timeout.
func (c *Client) downloadChartWithTimeout(
	spec *ChartSpec,
	chartName string,
	timeout time.Duration,
) (string, error) {
	// Create getter providers with custom timeout
	getterOpts := []helmv4getter.Option{
		helmv4getter.WithTimeout(timeout),
		helmv4getter.WithPassCredentialsAll(false),
		helmv4getter.WithTLSClientConfig(spec.CertFile, spec.KeyFile, spec.CaFile),
		helmv4getter.WithInsecureSkipVerifyTLS(spec.InsecureSkipTLSverify),
	}

	if spec.Username != "" && spec.Password != "" {
		getterOpts = append(getterOpts, helmv4getter.WithBasicAuth(spec.Username, spec.Password))
	}

	// Create chart downloader with extended timeout
	downloader := helmv4downloader.ChartDownloader{
		Out:              os.Stdout,
		Getters:          helmv4getter.All(c.settings, getterOpts...),
		Options:          getterOpts,
		RepositoryConfig: c.settings.RepositoryConfig,
		RepositoryCache:  c.settings.RepositoryCache,
		ContentCache:     c.settings.ContentCache,
	}

	// Ensure cache directories exist
	mkdirErr := os.MkdirAll(c.settings.RepositoryCache, repoDirMode)
	if mkdirErr != nil {
		return "", fmt.Errorf("failed to create repository cache: %w", mkdirErr)
	}

	// Find the chart URL in the repository
	chartURL, err := repov1.FindChartInRepoURL(
		spec.RepoURL,
		chartName,
		helmv4getter.All(c.settings, getterOpts...),
		repov1.WithChartVersion(spec.Version),
		repov1.WithUsernamePassword(spec.Username, spec.Password),
		repov1.WithClientTLS(spec.CertFile, spec.KeyFile, spec.CaFile),
		repov1.WithInsecureSkipTLSverify(spec.InsecureSkipTLSverify),
	)
	if err != nil {
		return "", fmt.Errorf("failed to find chart in repo: %w", err)
	}

	// Download the chart to cache
	filename, _, downloadErr := downloader.DownloadToCache(chartURL, spec.Version)
	if downloadErr != nil {
		return "", fmt.Errorf("failed to download chart: %w", downloadErr)
	}

	return filename, nil
}

func (c *Client) mergeValues(spec *ChartSpec, chartPath string) (map[string]any, error) {
	base := map[string]any{}

	err := mergeValueFiles(spec.ValueFiles, chartPath, base)
	if err != nil {
		return nil, err
	}

	err = mergeValuesYaml(spec.ValuesYaml, base)
	if err != nil {
		return nil, err
	}

	err = mergeSetValues(spec.SetValues, base)
	if err != nil {
		return nil, err
	}

	err = mergeSetJSONValues(spec.SetJSONVals, base)
	if err != nil {
		return nil, err
	}

	err = mergeSetFileValues(spec.SetFileVals, chartPath, base)
	if err != nil {
		return nil, err
	}

	return base, nil
}

func mergeValueFiles(valueFiles []string, chartPath string, base map[string]any) error {
	for _, filePath := range valueFiles {
		fileBytes, err := readFileFromPath(chartPath, filePath)
		if err != nil {
			return fmt.Errorf("failed to read values file %s: %w", filePath, err)
		}

		var parsedMap map[string]any

		err = yaml.Unmarshal(fileBytes, &parsedMap)
		if err != nil {
			return fmt.Errorf("failed to parse values file %s as YAML: %w", filePath, err)
		}

		if parsedMap != nil {
			mergeMapsInto(base, parsedMap)
		}
	}

	return nil
}

func mergeValuesYaml(valuesYaml string, base map[string]any) error {
	if valuesYaml == "" {
		return nil
	}

	var parsedMap map[string]any

	err := yaml.Unmarshal([]byte(valuesYaml), &parsedMap)
	if err != nil {
		return fmt.Errorf("failed to parse ValuesYaml: %w", err)
	}

	mergeMapsInto(base, parsedMap)

	return nil
}

func mergeSetValues(setValues map[string]string, base map[string]any) error {
	for key, val := range setValues {
		err := helmv4strvals.ParseInto(fmt.Sprintf("%s=%s", key, val), base)
		if err != nil {
			return fmt.Errorf("failed to parse set value %s=%s: %w", key, val, err)
		}
	}

	return nil
}

func mergeSetJSONValues(setJSONVals map[string]string, base map[string]any) error {
	for key, val := range setJSONVals {
		err := helmv4strvals.ParseJSON(fmt.Sprintf("%s=%s", key, val), base)
		if err != nil {
			return fmt.Errorf("failed to parse JSON value %s=%s: %w", key, val, err)
		}
	}

	return nil
}

func mergeSetFileValues(
	setFileVals map[string]string,
	chartPath string,
	base map[string]any,
) error {
	for key, filePath := range setFileVals {
		fileBytes, err := readFileFromPath(chartPath, filePath)
		if err != nil {
			return fmt.Errorf("failed to read file value %s: %w", filePath, err)
		}

		err = helmv4strvals.ParseInto(fmt.Sprintf("%s=%s", key, string(fileBytes)), base)
		if err != nil {
			return fmt.Errorf("failed to parse file value %s: %w", key, err)
		}
	}

	return nil
}

func readFileFromPath(chartPath, filePath string) ([]byte, error) {
	if filepath.IsAbs(filePath) {
		data, err := os.ReadFile(filePath) //nolint:gosec // filePath is validated by caller
		if err != nil {
			return nil, fmt.Errorf("read file %s: %w", filePath, err)
		}

		return data, nil
	}

	data, err := ksailio.ReadFileSafe(filepath.Dir(chartPath), filePath)
	if err != nil {
		return nil, fmt.Errorf("read file safe %s: %w", filePath, err)
	}

	return data, nil
}

func mergeMapsInto(dest, src map[string]any) {
	for key, srcVal := range src {
		if srcMap, ok := srcVal.(map[string]any); ok {
			if destVal, exists := dest[key]; exists {
				if destMap, ok := destVal.(map[string]any); ok {
					mergeMapsInto(destMap, srcMap)

					continue
				}
			}
		}

		dest[key] = srcVal
	}
}

func (c *Client) switchNamespace(namespace string) (func(), error) {
	if namespace == "" {
		return func() {}, nil
	}

	previousNamespace := c.settings.Namespace()
	if previousNamespace == namespace {
		return func() {}, nil
	}

	c.settings.SetNamespace(namespace)

	reinitErr := c.actionConfig.Init(
		c.settings.RESTClientGetter(),
		namespace,
		os.Getenv("HELM_DRIVER"),
	)
	if reinitErr != nil {
		_ = c.restoreNamespace(previousNamespace)

		return nil, fmt.Errorf("failed to set helm namespace %q: %w", namespace, reinitErr)
	}

	return func() {
		restoreErr := c.restoreNamespace(previousNamespace)
		if restoreErr != nil {
			c.debugLog("failed to restore helm namespace: %v", restoreErr)
		}
	}, nil
}

func (c *Client) restoreNamespace(namespace string) error {
	c.settings.SetNamespace(namespace)

	err := c.actionConfig.Init(
		c.settings.RESTClientGetter(),
		namespace,
		os.Getenv("HELM_DRIVER"),
	)
	if err != nil {
		return fmt.Errorf("init action config for namespace %s: %w", namespace, err)
	}

	return nil
}

func parseChartRef(chartRef string) (string, string) {
	parts := strings.SplitN(chartRef, "/", chartRefParts)
	if len(parts) == 1 {
		return "", parts[0]
	}

	return parts[0], parts[1]
}

func releaseToInfo(rel *v1.Release) *ReleaseInfo {
	if rel == nil {
		return nil
	}

	return &ReleaseInfo{
		Name:       rel.Name,
		Namespace:  rel.Namespace,
		Revision:   rel.Version,
		Status:     rel.Info.Status.String(),
		Chart:      rel.Chart.Metadata.Name,
		AppVersion: rel.Chart.Metadata.AppVersion,
		Updated:    rel.Info.LastDeployed,
		Notes:      rel.Info.Notes,
	}
}

func runWithSilencedStderr(
	operation func() (any, error),
) (any, error) {
	readPipe, writePipe, pipeErr := os.Pipe()
	if pipeErr != nil {
		return operation()
	}

	stderrCaptureMu.Lock()
	defer stderrCaptureMu.Unlock()

	originalStderr := os.Stderr

	var (
		stderrBuffer bytes.Buffer
		waitGroup    sync.WaitGroup
	)

	waitGroup.Add(1)
	waitGroup.Go(func() {
		_, _ = io.Copy(&stderrBuffer, readPipe)
	})

	os.Stderr = writePipe

	var (
		releaseResult any
		runErr        error
	)

	defer func() {
		_ = writePipe.Close()

		waitGroup.Wait()

		_ = readPipe.Close()
		os.Stderr = originalStderr

		if runErr != nil {
			logs := strings.TrimSpace(stderrBuffer.String())
			if logs != "" {
				runErr = fmt.Errorf("%w: %s", runErr, logs)
			}
		}
	}()

	releaseResult, runErr = operation()

	return releaseResult, runErr
}
