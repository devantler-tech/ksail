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
chartv2 "helm.sh/helm/v4/pkg/chart/v2"
helmv4loader "helm.sh/helm/v4/pkg/chart/loader"
helmv4cli "helm.sh/helm/v4/pkg/cli"
helmv4getter "helm.sh/helm/v4/pkg/getter"
helmv4kube "helm.sh/helm/v4/pkg/kube"
v1 "helm.sh/helm/v4/pkg/release/v1"
repov1 "helm.sh/helm/v4/pkg/repo/v1"
helmv4strvals "helm.sh/helm/v4/pkg/strvals"
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
)

// stderrCaptureMu protects process-wide stderr redirection from concurrent access.
var stderrCaptureMu sync.Mutex //nolint:gochecknoglobals // global lock required to coordinate stderr interception

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
	AddRepository(ctx context.Context, entry *RepositoryEntry) error
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
func (c *Client) AddRepository(ctx context.Context, entry *RepositoryEntry) error {
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

	chartRepository, err := newChartRepository(settings, repoEntry, repoCache)
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
) (*repov1.ChartRepository, error) {
chartRepository, err := repov1.NewChartRepository(repoEntry, helmv4getter.All(settings))
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
	if spec.Wait {
		client.WaitStrategy = helmv4kube.StatusWatcherStrategy
	}
	client.WaitForJobs = spec.WaitForJobs
	client.Timeout = spec.Timeout
	if client.Timeout == 0 {
		client.Timeout = DefaultTimeout
	}
	// Note: Atomic is not supported in Helm v4 Install action
	client.Version = spec.Version

	chart, vals, err := c.loadChartAndValues(spec, client)
	if err != nil {
		return nil, err
	}

	runFn := func() (interface{}, error) {
		return client.RunWithContext(ctx, chart, vals)
	}

	return executeAndExtractRelease(runFn, spec.Silent)
}

func (c *Client) upgradeRelease(ctx context.Context, spec *ChartSpec) (*v1.Release, error) {
	client := helmv4action.NewUpgrade(c.actionConfig)
	client.Namespace = spec.Namespace
	if spec.Wait {
		client.WaitStrategy = helmv4kube.StatusWatcherStrategy
	}
	client.WaitForJobs = spec.WaitForJobs
	client.Timeout = spec.Timeout
	if client.Timeout == 0 {
		client.Timeout = DefaultTimeout
	}
	// Note: Atomic is not supported in Helm v4 Upgrade action
	client.Version = spec.Version
	client.SkipCRDs = !spec.UpgradeCRDs // Inverted logic in v4

	chart, vals, err := c.loadChartAndValues(spec, client)
	if err != nil {
		return nil, err
	}

	runFn := func() (interface{}, error) {
		return client.RunWithContext(ctx, spec.ReleaseName, chart, vals)
	}

	return executeAndExtractRelease(runFn, spec.Silent)
}

func (c *Client) loadChartAndValues(
	spec *ChartSpec,
	client interface{},
) (*chartv2.Chart, map[string]interface{}, error) {
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
	runFn func() (interface{}, error),
	silent bool,
) (*v1.Release, error) {
	var releaser interface{}
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
		return nil, fmt.Errorf("unexpected release type: %T", releaser)
	}

	return rel, nil
}

func (c *Client) locateAndLoadChart(spec *ChartSpec, client interface{}) (string, *chartv2.Chart, error) {
var chartPath string
var err error

if spec.RepoURL != "" {
chartPath, err = c.locateChartFromRepo(spec, client)
} else {
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
return "", nil, fmt.Errorf("unexpected chart type: %T", chartInterface)
}

return chartPath, chart, nil
}

func (c *Client) locateChartFromRepo(spec *ChartSpec, client interface{}) (string, error) {
	_, chartName := parseChartRef(spec.ChartName)
	if chartName == "" {
		chartName = spec.ChartName
	}

	// Set ChartPathOptions for the action client
	setChartPathOptions(client, spec)

	// Use FindChartInRepoURL with options
	options := []repov1.FindChartInRepoURLOption{
		repov1.WithChartVersion(spec.Version),
	}

	if spec.Username != "" || spec.Password != "" {
		options = append(options, repov1.WithUsernamePassword(spec.Username, spec.Password))
	}

	if spec.CertFile != "" || spec.KeyFile != "" || spec.CaFile != "" {
		options = append(options, repov1.WithClientTLS(spec.CertFile, spec.KeyFile, spec.CaFile))
	}

	if spec.InsecureSkipTLSverify {
		options = append(options, repov1.WithInsecureSkipTLSverify(spec.InsecureSkipTLSverify))
	}

	chartURL, err := repov1.FindChartInRepoURL(
		spec.RepoURL,
		chartName,
		helmv4getter.All(c.settings),
		options...,
	)
	if err != nil {
		return "", fmt.Errorf(
			"failed to locate chart %q in repository %s: %w",
			chartName,
			spec.RepoURL,
			err,
		)
	}

	return chartURL, nil
}

// chartPathOptionsSetter defines the interface for setting chart path options.
type chartPathOptionsSetter interface {
	SetChartPathOptions(opts helmv4action.ChartPathOptions)
	GetChartPathOptions() helmv4action.ChartPathOptions
}

func setChartPathOptions(client interface{}, spec *ChartSpec) {
	opts := helmv4action.ChartPathOptions{
		RepoURL:               spec.RepoURL,
		Username:              spec.Username,
		Password:              spec.Password,
		CertFile:              spec.CertFile,
		KeyFile:               spec.KeyFile,
		CaFile:                spec.CaFile,
		InsecureSkipTLSverify: spec.InsecureSkipTLSverify,
	}

	switch cl := client.(type) {
	case *helmv4action.Install:
		cl.ChartPathOptions = opts
	case *helmv4action.Upgrade:
		cl.ChartPathOptions = opts
	}
}

func (c *Client) mergeValues(spec *ChartSpec, chartPath string) (map[string]interface{}, error) {
	base := map[string]interface{}{}

	// Load values from files
	for _, filePath := range spec.ValueFiles {
		fileBytes, err := readFileFromPath(chartPath, filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read values file %s: %w", filePath, err)
		}

		parsedMap, err := helmv4strvals.ParseString(string(fileBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to parse values file %s: %w", filePath, err)
		}

		base = mergeMaps(base, parsedMap)
	}

	// Merge ValuesYaml
	if spec.ValuesYaml != "" {
		parsedMap, err := helmv4strvals.ParseString(spec.ValuesYaml)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ValuesYaml: %w", err)
		}
		base = mergeMaps(base, parsedMap)
	}

	// Merge SetValues
	for key, val := range spec.SetValues {
		if err := helmv4strvals.ParseInto(fmt.Sprintf("%s=%s", key, val), base); err != nil {
			return nil, fmt.Errorf("failed to parse set value %s=%s: %w", key, val, err)
		}
	}

	// Merge SetJSONVals
	for key, val := range spec.SetJSONVals {
		if err := helmv4strvals.ParseJSON(fmt.Sprintf("%s=%s", key, val), base); err != nil {
			return nil, fmt.Errorf("failed to parse JSON value %s=%s: %w", key, val, err)
		}
	}

	// Merge SetFileVals
	for key, filePath := range spec.SetFileVals {
		fileBytes, err := readFileFromPath(chartPath, filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file value %s: %w", filePath, err)
		}

		if err := helmv4strvals.ParseInto(fmt.Sprintf("%s=%s", key, string(fileBytes)), base); err != nil {
			return nil, fmt.Errorf("failed to parse file value %s: %w", key, err)
		}
	}

	return base, nil
}

func readFileFromPath(chartPath, filePath string) ([]byte, error) {
	if filepath.IsAbs(filePath) {
		return os.ReadFile(filePath)
	}
	return ksailio.ReadFileSafe(filepath.Dir(chartPath), filePath)
}

func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
out := make(map[string]interface{}, len(a))
for k, v := range a {
out[k] = v
}
for k, v := range b {
if v, ok := v.(map[string]interface{}); ok {
if bv, ok := out[k]; ok {
if bv, ok := bv.(map[string]interface{}); ok {
out[k] = mergeMaps(bv, v)
continue
}
}
}
out[k] = v
}
return out
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
		c.restoreNamespace(previousNamespace)
		return nil, fmt.Errorf("failed to set helm namespace %q: %w", namespace, reinitErr)
	}

	return func() {
		if restoreErr := c.restoreNamespace(previousNamespace); restoreErr != nil {
			c.debugLog("failed to restore helm namespace: %v", restoreErr)
		}
	}, nil
}

func (c *Client) restoreNamespace(namespace string) error {
	c.settings.SetNamespace(namespace)
	return c.actionConfig.Init(
		c.settings.RESTClientGetter(),
		namespace,
		os.Getenv("HELM_DRIVER"),
	)
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
operation func() (interface{}, error),
) (interface{}, error) {
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

//nolint:modernize // sync.WaitGroup does not have Go() method; that's only in errgroup.Group
waitGroup.Add(1)

go func() {
defer waitGroup.Done()

_, _ = io.Copy(&stderrBuffer, readPipe)
}()

os.Stderr = writePipe

var (
releaseResult interface{}
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
