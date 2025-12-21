package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	ksailio "github.com/devantler-tech/ksail/v5/pkg/io"
	helmclientlib "github.com/mittwald/go-helm-client"
	valueslib "github.com/mittwald/go-helm-client/values"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
)

const (
	// DefaultTimeout defines the fallback Helm chart installation timeout.
	DefaultTimeout = 5 * time.Minute
	repoDirMode    = 0o750
	repoFileMode   = 0o640
	chartRefParts  = 2
)

var (
	errUnsupportedClientImplementation = errors.New("helm: unsupported client implementation")
	errReleaseNameRequired             = errors.New("helm: release name is required")
	errRepositoryEntryRequired         = errors.New("helm: repository entry is required")
	errRepositoryNameRequired          = errors.New("helm: repository name is required")
	errRepositoryCacheUnset            = errors.New("helm: repository cache path is not set")
	errRepositoryConfigUnset           = errors.New("helm: repository config path is not set")
	errChartSpecRequired               = errors.New("helm: chart spec is required")
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
	inner helmclientlib.Client
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
	inner, err := createHelmClient(kubeConfig, kubeContext, debug)
	if err != nil {
		return nil, err
	}

	return &Client{inner: inner}, nil
}

func createHelmClient(
	kubeConfig, kubeContext string,
	debug func(string, ...any),
) (*helmclientlib.HelmClient, error) {
	debugLog := debug
	if debugLog == nil {
		debugLog = func(string, ...any) {}
	}

	options := &helmclientlib.Options{
		Linting:  false,
		Debug:    debug != nil,
		DebugLog: debugLog,
	}

	if kubeConfig != "" {
		data, readErr := ksailio.ReadFileSafe(filepath.Dir(kubeConfig), kubeConfig)
		if readErr == nil {
			configuredClient, err := newHelmClientFromKubeConf(options, data, kubeContext)
			if err == nil {
				return configuredClient, nil
			}
		}
	}

	concrete, err := newHelmClient(options)
	if err != nil {
		return nil, err
	}

	settings := concrete.GetSettings()
	if kubeConfig != "" {
		settings.KubeConfig = kubeConfig
	}

	if kubeContext != "" {
		settings.KubeContext = kubeContext
	}

	reinitErr := reinitActionConfig(concrete)
	if reinitErr != nil {
		return nil, fmt.Errorf("failed to initialize helm action config: %w", reinitErr)
	}

	return concrete, nil
}

func newHelmClientFromKubeConf(
	options *helmclientlib.Options,
	kubeConfig []byte,
	kubeContext string,
) (*helmclientlib.HelmClient, error) {
	// We must keep the mutex held for the entire duration because os.Stderr is a
	// process-wide global. Releasing the lock before restoration would allow
	// another goroutine to swap stderr out from under us, leading to corrupted
	// logs or panics when we attempt to restore the original writer.
	stderrCaptureMu.Lock()
	defer stderrCaptureMu.Unlock()

	client, err := helmclientlib.NewClientFromKubeConf(&helmclientlib.KubeConfClientOptions{
		Options:     options,
		KubeConfig:  kubeConfig,
		KubeContext: kubeContext,
	})
	if err != nil {
		return nil, fmt.Errorf("create helm client from kubeconfig: %w", err)
	}

	return ensureHelmClient(client)
}

func newHelmClient(options *helmclientlib.Options) (*helmclientlib.HelmClient, error) {
	stderrCaptureMu.Lock()
	defer stderrCaptureMu.Unlock()

	client, err := helmclientlib.New(options)
	if err != nil {
		return nil, fmt.Errorf("failed to create helm client: %w", err)
	}

	return ensureHelmClient(client)
}

func ensureHelmClient(client helmclientlib.Client) (*helmclientlib.HelmClient, error) {
	helConcrete, ok := client.(*helmclientlib.HelmClient)
	if !ok {
		return nil, errUnsupportedClientImplementation
	}

	return helConcrete, nil
}

func reinitActionConfig(helmClient *helmclientlib.HelmClient) error {
	reinitErr := helmClient.ActionConfig.Init(
		helmClient.Settings.RESTClientGetter(),
		helmClient.Settings.Namespace(),
		os.Getenv("HELM_DRIVER"),
		helmClient.DebugLog,
	)
	if reinitErr != nil {
		return fmt.Errorf("initialize helm action config: %w", reinitErr)
	}

	return nil
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

	chartSpec := &helmclientlib.ChartSpec{
		ReleaseName: releaseName,
		Namespace:   namespace,
	}

	uninstallErr := c.inner.UninstallRelease(chartSpec)
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

	settings := c.inner.GetSettings()

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

func ensureRepositoryConfig(settings *cli.EnvSettings) (string, error) {
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

func loadOrInitRepositoryFile(repoFile string) *repo.File {
	repositoryFile, err := repo.LoadFile(repoFile)
	if err != nil {
		return repo.NewFile()
	}

	return repositoryFile
}

func convertRepositoryEntry(entry *RepositoryEntry) *repo.Entry {
	return &repo.Entry{
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

func ensureRepositoryCache(settings *cli.EnvSettings) (string, error) {
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
	settings *cli.EnvSettings,
	repoEntry *repo.Entry,
	repoCache string,
) (*repo.ChartRepository, error) {
	chartRepository, err := repo.NewChartRepository(repoEntry, getter.All(settings))
	if err != nil {
		return nil, fmt.Errorf("create chart repository: %w", err)
	}

	chartRepository.CachePath = repoCache

	return chartRepository, nil
}

func downloadRepositoryIndex(chartRepository *repo.ChartRepository) error {
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
	return c.executeReleaseOp(
		ctx,
		spec,
		true,
		func(ctx context.Context, chartSpec *helmclientlib.ChartSpec) (*release.Release, error) {
			if upgrade {
				return c.inner.InstallOrUpgradeChart(ctx, chartSpec, nil)
			}

			return c.inner.InstallChart(ctx, chartSpec, nil)
		},
	)
}

func (c *Client) switchNamespace(namespace string) (func(), error) {
	if namespace == "" {
		return func() {}, nil
	}

	helmClient, err := c.concreteClient()
	if err != nil {
		return nil, err
	}

	settings := helmClient.Settings

	previous := settings.Namespace()
	if previous == namespace {
		return func() {}, nil
	}

	settings.SetNamespace(namespace)

	reinitErr := reinitActionConfig(helmClient)
	if reinitErr != nil {
		settings.SetNamespace(previous)

		_ = reinitActionConfig(helmClient)

		return nil, fmt.Errorf("failed to set helm namespace %q: %w", namespace, reinitErr)
	}

	return func() {
		settings.SetNamespace(previous)

		restoreErr := reinitActionConfig(helmClient)
		if restoreErr != nil {
			helmClient.DebugLog("failed to restore helm namespace: %v", restoreErr)
		}
	}, nil
}

func (c *Client) concreteClient() (*helmclientlib.HelmClient, error) {
	implementation, ok := c.inner.(*helmclientlib.HelmClient)
	if !ok {
		return nil, errUnsupportedClientImplementation
	}

	return implementation, nil
}

func (c *Client) executeReleaseOp(
	ctx context.Context,
	spec *ChartSpec,
	applyDefaultTimeout bool,
	operation func(context.Context, *helmclientlib.ChartSpec) (*release.Release, error),
) (*ReleaseInfo, error) {
	chartSpec, cleanup, err := c.prepareChartSpec(spec, applyDefaultTimeout)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	run := func() (*release.Release, error) {
		return operation(ctx, chartSpec)
	}

	var rel *release.Release
	if spec != nil && spec.Silent {
		rel, err = runReleaseWithSilencedStderr(run)
	} else {
		rel, err = run()
	}

	if err != nil {
		return nil, err
	}

	return releaseToInfo(rel), nil
}

func (c *Client) prepareChartSpec(
	spec *ChartSpec,
	applyDefaultTimeout bool,
) (*helmclientlib.ChartSpec, func(), error) {
	if spec == nil {
		return nil, nil, errChartSpecRequired
	}

	chartSpec := convertChartSpec(spec)
	if applyDefaultTimeout && chartSpec.Timeout == 0 {
		chartSpec.Timeout = DefaultTimeout
	}

	ensureErr := c.ensureRepository(spec, chartSpec)
	if ensureErr != nil {
		return nil, nil, ensureErr
	}

	cleanup, err := c.switchNamespace(chartSpec.Namespace)
	if err != nil {
		return nil, nil, err
	}

	return chartSpec, cleanup, nil
}

func (c *Client) ensureRepository(spec *ChartSpec, chartSpec *helmclientlib.ChartSpec) error {
	if spec.RepoURL == "" {
		return nil
	}

	_, chartName := parseChartRef(spec.ChartName)

	if chartName == "" {
		chartName = spec.ChartName
	}

	settings := c.inner.GetSettings()

	chartURL, err := repo.FindChartInAuthAndTLSAndPassRepoURL(
		spec.RepoURL,
		spec.Username,
		spec.Password,
		chartName,
		spec.Version,
		spec.CertFile,
		spec.KeyFile,
		spec.CaFile,
		spec.InsecureSkipTLSverify,
		false,
		getter.All(settings),
	)
	if err != nil {
		return fmt.Errorf(
			"failed to locate chart %q in repository %s: %w",
			chartName,
			spec.RepoURL,
			err,
		)
	}

	chartSpec.ChartName = chartURL

	return nil
}

func convertChartSpec(spec *ChartSpec) *helmclientlib.ChartSpec {
	return &helmclientlib.ChartSpec{
		ReleaseName: spec.ReleaseName,
		ChartName:   spec.ChartName,
		Namespace:   spec.Namespace,
		ValuesYaml:  spec.ValuesYaml,
		ValuesOptions: valueslib.Options{
			ValueFiles:   copyStringSlice(spec.ValueFiles),
			StringValues: convertMapToSlice(spec.SetValues),
			FileValues:   convertMapToSlice(spec.SetFileVals),
			JSONValues:   convertMapToSlice(spec.SetJSONVals),
		},
		Version:         spec.Version,
		CreateNamespace: spec.CreateNamespace,
		Wait:            spec.Wait,
		WaitForJobs:     spec.WaitForJobs,
		Timeout:         spec.Timeout,
		Atomic:          spec.Atomic,
		UpgradeCRDs:     spec.UpgradeCRDs,
	}
}

func convertMapToSlice(valuesMap map[string]string) []string {
	if len(valuesMap) == 0 {
		return nil
	}

	keys := make([]string, 0, len(valuesMap))
	for key := range valuesMap {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, fmt.Sprintf("%s=%s", key, valuesMap[key]))
	}

	return result
}

func copyStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}

	dup := make([]string, len(src))
	copy(dup, src)

	return dup
}

func parseChartRef(chartRef string) (string, string) {
	parts := strings.SplitN(chartRef, "/", chartRefParts)
	if len(parts) == 1 {
		return "", parts[0]
	}

	return parts[0], parts[1]
}

func releaseToInfo(rel *release.Release) *ReleaseInfo {
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
		Updated:    rel.Info.LastDeployed.Time,
		Notes:      rel.Info.Notes,
	}
}

func runReleaseWithSilencedStderr(
	operation func() (*release.Release, error),
) (*release.Release, error) {
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
		releaseResult *release.Release
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
