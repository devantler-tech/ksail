package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	helmv4action "helm.sh/helm/v4/pkg/action"
	helmv4loader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	helmv4cli "helm.sh/helm/v4/pkg/cli"
	helmv4kube "helm.sh/helm/v4/pkg/kube"
	helmv4registry "helm.sh/helm/v4/pkg/registry"
	helmv4release "helm.sh/helm/v4/pkg/release"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	helmv4driver "helm.sh/helm/v4/pkg/storage/driver"
)

const (
	// DefaultTimeout defines the fallback Helm chart installation timeout.
	DefaultTimeout = 5 * time.Minute
	chartRefParts  = 2
)

var (
	errReleaseNameRequired   = errors.New("helm: release name is required")
	errChartSpecRequired     = errors.New("helm: chart spec is required")
	errUnexpectedReleaseType = errors.New("helm: unexpected release type")
	errUnexpectedChartType   = errors.New("helm: unexpected chart type")
	errUnsupportedClientType = errors.New("helm: unsupported client type for OCI chart")
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
//
// When spec.Wait is true, this function configures the action to use
// StatusWatcherStrategy, which leverages kstatus (HIP-0022) for enhanced
// resource waiting. kstatus provides:
//   - Support for custom resources (via the ready condition)
//   - Full reconciliation monitoring (including cleanup of old pods)
//   - Consistent status checking across all resource types
//
// See: https://helm.sh/community/hips/hip-0022/
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
type Interface interface {
	InstallChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error)
	InstallOrUpgradeChart(ctx context.Context, spec *ChartSpec) (*ReleaseInfo, error)
	UninstallRelease(ctx context.Context, releaseName, namespace string) error
	AddRepository(ctx context.Context, entry *RepositoryEntry, timeout time.Duration) error
	TemplateChart(ctx context.Context, spec *ChartSpec) (string, error)
	ReleaseExists(ctx context.Context, releaseName, namespace string) (bool, error)
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

// NewTemplateOnlyClient creates a Helm client for templating operations only.
// It does not require a kubeconfig and cannot perform install/uninstall operations.
// Use this for extracting images from charts in CI environments without cluster access.
func NewTemplateOnlyClient() (*Client, error) {
	settings := helmv4cli.New()
	actionConfig := new(helmv4action.Configuration)

	// Initialize with a no-op kube client for templating-only operations
	actionConfig.KubeClient = &helmv4kube.Client{}
	actionConfig.Releases = nil // Not needed for templating

	// Initialize registry client so OCI chart references (oci://) can be resolved
	registryClient, err := helmv4registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client for template client: %w", err)
	}

	actionConfig.RegistryClient = registryClient

	return &Client{
		actionConfig: actionConfig,
		settings:     settings,
		kubeConfig:   "",
		kubeContext:  "",
		debugLog:     func(string, ...any) {},
	}, nil
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

// TemplateChart renders a Helm chart's templates without installing it.
// It returns the rendered YAML manifests as a string.
// This is useful for extracting container images from charts.
func (c *Client) TemplateChart(ctx context.Context, spec *ChartSpec) (string, error) {
	if spec == nil {
		return "", errChartSpecRequired
	}

	ctxErr := ctx.Err()
	if ctxErr != nil {
		return "", fmt.Errorf("template chart context cancelled: %w", ctxErr)
	}

	client := helmv4action.NewInstall(c.actionConfig)

	client.ReleaseName = spec.ReleaseName
	if client.ReleaseName == "" {
		client.ReleaseName = "template-release"
	}

	client.Namespace = spec.Namespace
	if client.Namespace == "" {
		client.Namespace = "default"
	}

	client.DryRunStrategy = helmv4action.DryRunClient
	client.Replace = true // Skip name uniqueness check

	// Set version if provided
	if spec.Version != "" {
		client.Version = spec.Version
	}

	chart, vals, err := c.loadChartAndValues(spec, client)
	if err != nil {
		return "", fmt.Errorf("load chart and values: %w", err)
	}

	rel, err := client.RunWithContext(ctx, chart, vals)
	if err != nil {
		return "", fmt.Errorf("template chart %q: %w", spec.ChartName, err)
	}

	// Convert Releaser to Accessor to get the manifest
	accessor, accErr := helmv4release.NewAccessor(rel)
	if accErr != nil {
		return "", fmt.Errorf("create release accessor: %w", accErr)
	}

	return accessor.Manifest(), nil
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

// ReleaseExists checks whether a Helm release with the given name exists in the
// specified namespace. It returns true when at least one revision is recorded.
func (c *Client) ReleaseExists(
	_ context.Context,
	releaseName, namespace string,
) (bool, error) {
	if releaseName == "" {
		return false, errReleaseNameRequired
	}

	cleanup, err := c.switchNamespace(namespace)
	if err != nil {
		return false, err
	}

	defer cleanup()

	histClient := helmv4action.NewHistory(c.actionConfig)
	histClient.Max = 1

	releases, err := histClient.Run(releaseName)
	if err != nil {
		if errors.Is(err, helmv4driver.ErrReleaseNotFound) {
			return false, nil
		}

		return false, fmt.Errorf("failed to check release history for %q: %w", releaseName, err)
	}

	return len(releases) > 0, nil
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
		InsecureSkipTLSVerify: spec.InsecureSkipTLSverify,
	}
}

// chartLocator abstracts the common chart location capabilities shared by
// helmv4action.Install and helmv4action.Upgrade.
type chartLocator interface {
	SetRegistryClient(client *helmv4registry.Client)
	LocateChart(name string, settings *helmv4cli.EnvSettings) (string, error)
}

// applyChartPathOptions applies ChartPathOptions to an Install or Upgrade client.
func applyChartPathOptions(client any, opts helmv4action.ChartPathOptions) {
	switch cl := client.(type) {
	case *helmv4action.Install:
		cl.ChartPathOptions = opts
	case *helmv4action.Upgrade:
		cl.ChartPathOptions = opts
	}
}

func (c *Client) locateOCIChart(spec *ChartSpec, client any) (string, error) {
	registryClient, err := helmv4registry.NewClient()
	if err != nil {
		return "", fmt.Errorf("failed to create registry client: %w", err)
	}

	applyChartPathOptions(client, buildChartPathOptions(spec, ""))

	locator, ok := client.(chartLocator)
	if !ok {
		return "", fmt.Errorf("%w: %T", errUnsupportedClientType, client)
	}

	locator.SetRegistryClient(registryClient)

	chartPath, err := locator.LocateChart(spec.ChartName, c.settings)
	if err != nil {
		return "", fmt.Errorf("failed to locate OCI chart %q: %w", spec.ChartName, err)
	}

	return chartPath, nil
}

func (c *Client) locateChartFromRepo(spec *ChartSpec, client any) (string, error) {
	_, chartName := parseChartRef(spec.ChartName)
	if chartName == "" {
		chartName = spec.ChartName
	}

	// Set HELM_HTTP_TIMEOUT to override the default 120-second timeout.
	// Large charts (like Calico/Tigera) can take longer to download.
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	originalTimeout, hadTimeout := os.LookupEnv("HELM_HTTP_TIMEOUT")

	err := os.Setenv("HELM_HTTP_TIMEOUT", timeout.String())
	if err != nil {
		return "", fmt.Errorf("failed to set HELM_HTTP_TIMEOUT: %w", err)
	}

	defer func() {
		if hadTimeout {
			_ = os.Setenv("HELM_HTTP_TIMEOUT", originalTimeout)
		} else {
			_ = os.Unsetenv("HELM_HTTP_TIMEOUT")
		}
	}()

	opts := buildChartPathOptions(spec, spec.RepoURL)
	applyChartPathOptions(client, opts)

	// Use LocateChart to download the chart to cache and return the local path
	chartPath, err := opts.LocateChart(chartName, c.settings)
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
