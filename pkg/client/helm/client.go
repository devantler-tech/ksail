package helm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	helmv4action "helm.sh/helm/v4/pkg/action"
	helmv4cli "helm.sh/helm/v4/pkg/cli"
	helmv4kube "helm.sh/helm/v4/pkg/kube"
	helmv4registry "helm.sh/helm/v4/pkg/registry"
	helmv4release "helm.sh/helm/v4/pkg/release"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	helmv4driver "helm.sh/helm/v4/pkg/storage/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// DefaultTimeout defines the fallback Helm chart installation timeout.
	DefaultTimeout = 5 * time.Minute
)

// ValidateKubernetesReleaseStorageDriver rejects Helm backends whose release
// records have no Kubernetes UID. Identity-bound lifecycle operations must run
// only with Secret or ConfigMap storage so they can distinguish a same-name
// replacement from the release incarnation KSail actually mutated.
func ValidateKubernetesReleaseStorageDriver(driver string) error {
	driver = strings.ToLower(strings.TrimSpace(driver))
	switch driver {
	case "", "secret", "secrets", "configmap", "configmaps":
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrReleaseStorageDriverUnsupported, driver)
	}
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

// RefreshDiscovery invalidates cached Kubernetes API discovery so subsequent
// operations observe CRDs (and other API resources) registered since the client
// was created. See the Interface documentation for why this is required for
// multi-chart installs that separate CRDs from the resources that use them.
func (c *Client) RefreshDiscovery() error {
	// Drop the on-disk discovery cache shared across RESTClientGetter instances
	// for this cluster, so a freshly built RESTMapper re-reads it from the API.
	// Fail fast if the discovery client can't be built rather than proceeding with a
	// possibly stale cache (which would resurface the CRD/RESTMapper race downstream).
	discoveryClient, err := c.settings.RESTClientGetter().ToDiscoveryClient()
	if err != nil {
		return fmt.Errorf("get discovery client for cache invalidation: %w", err)
	}

	discoveryClient.Invalidate()

	// Rebuild settings to obtain a RESTClientGetter with a fresh, unmemoized
	// RESTMapper. genericclioptions.ConfigFlags memoizes both its discovery
	// client and REST mapper, so reusing the existing getter would keep serving
	// the stale in-memory mapping even after the on-disk cache is invalidated.
	// Preserve the current namespace so this only refreshes discovery and does
	// not reset behavioral config set elsewhere (e.g. via SetNamespace).
	namespace := c.settings.Namespace()

	settings := helmv4cli.New()
	if c.kubeConfig != "" {
		settings.KubeConfig = c.kubeConfig
	}

	if c.kubeContext != "" {
		settings.KubeContext = c.kubeContext
	}

	settings.SetNamespace(namespace)

	c.settings = settings

	initErr := c.actionConfig.Init(
		settings.RESTClientGetter(),
		settings.Namespace(),
		os.Getenv("HELM_DRIVER"),
	)
	if initErr != nil {
		return fmt.Errorf("reinitialize helm action config after discovery refresh: %w", initErr)
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

	chart, vals, err := c.loadChartAndValues(ctx, spec, client)
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

// ReleaseExists checks whether the latest Helm release revision with the given
// name is deployed in the specified namespace. Failed, pending, uninstalled,
// and superseded history must not be treated as a live component.
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

	releases, err := histClient.Run(releaseName)
	if err != nil {
		if errors.Is(err, helmv4driver.ErrReleaseNotFound) {
			return false, nil
		}

		return false, fmt.Errorf("failed to check release history for %q: %w", releaseName, err)
	}

	latestVersion := -1
	latestStatus := ""

	for _, release := range releases {
		accessor, accessorErr := helmv4release.NewAccessor(release)
		if accessorErr != nil {
			return false, fmt.Errorf(
				"failed to access release history for %q: %w",
				releaseName,
				accessorErr,
			)
		}

		if accessor.Version() > latestVersion {
			latestVersion = accessor.Version()
			latestStatus = accessor.Status()
		}
	}

	return latestVersion >= 0 && strings.EqualFold(latestStatus, "deployed"), nil
}

// ListReleases returns Helm releases across all namespaces for all statuses in a
// single Kubernetes API call. Use this instead of multiple ReleaseExists calls
// to reduce API roundtrips when detecting many components at once.
func (c *Client) ListReleases(ctx context.Context) ([]ReleaseInfo, error) {
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("list releases context cancelled: %w", err)
	}

	if c.actionConfig == nil || c.actionConfig.Releases == nil {
		return nil, errListReleasesUnsupported
	}

	// Helm v4's List.AllNamespaces field is declared but never referenced in
	// Run(), so setting it has no effect. The only way to query releases across
	// all namespaces is to reinitialise the action configuration with an empty
	// namespace, which causes the underlying Secrets storage driver to call
	// client.CoreV1().Secrets("") — equivalent to v1.NamespaceAll.
	previousNamespace := c.settings.Namespace()
	c.settings.SetNamespace("")

	reinitErr := c.actionConfig.Init(
		c.settings.RESTClientGetter(),
		"",
		os.Getenv("HELM_DRIVER"),
	)
	if reinitErr != nil {
		restoreErr := c.restoreNamespace(previousNamespace)
		if restoreErr != nil {
			c.debugLog("failed to restore helm namespace after list init failure: %v", restoreErr)
		}

		return nil, fmt.Errorf("failed to list helm releases: %w", reinitErr)
	}

	defer func() {
		restoreErr := c.restoreNamespace(previousNamespace)
		if restoreErr != nil {
			c.debugLog("failed to restore helm namespace after listing releases: %v", restoreErr)
		}
	}()

	listClient := helmv4action.NewList(c.actionConfig)
	listClient.All = true

	releases, err := listClient.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to list helm releases: %w", err)
	}

	result := make([]ReleaseInfo, 0, len(releases))

	for _, rel := range releases {
		accessor, accErr := helmv4release.NewAccessor(rel)
		if accErr != nil {
			return nil, fmt.Errorf("failed to access helm release from list result: %w", accErr)
		}

		result = append(result, ReleaseInfo{
			Name:      accessor.Name(),
			Namespace: accessor.Namespace(),
			Revision:  accessor.Version(),
			Status:    accessor.Status(),
		})
	}

	return result, nil
}

// GetReleaseStorageLabels returns the Kubernetes object labels from the latest
// Helm release storage object (Secret or ConfigMap) for the given release name
// and namespace. The storage backend is determined by the HELM_DRIVER
// environment variable: "configmap"/"configmaps" queries ConfigMaps, "memory"
// always returns ErrNoReleaseStorage (no Kubernetes objects), and the default
// queries Secrets. Returns (nil, ErrNoReleaseStorage) when no matching storage
// objects exist.
func (c *Client) GetReleaseStorageLabels(
	ctx context.Context,
	releaseName, namespace string,
) (map[string]string, error) {
	metadata, err := c.GetReleaseStorageMetadata(ctx, releaseName, namespace)
	if err != nil {
		return nil, err
	}

	return metadata.Labels, nil
}

// GetReleaseStorageMetadata returns labels and the Kubernetes UID from the
// latest Helm release storage object (Secret or ConfigMap).
func (c *Client) GetReleaseStorageMetadata(
	ctx context.Context,
	releaseName, namespace string,
) (*ReleaseStorageMetadata, error) {
	if releaseName == "" {
		return nil, errReleaseNameRequired
	}

	if namespace == "" {
		namespace = c.settings.Namespace()
	}

	driver := os.Getenv("HELM_DRIVER")

	err := ValidateKubernetesReleaseStorageDriver(driver)
	if err != nil {
		return nil, err
	}

	restConfig, err := c.settings.RESTClientGetter().ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("get REST config for release storage labels: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes clientset for release storage labels: %w", err)
	}

	selector := fmt.Sprintf("name=%s,owner=helm", releaseName)

	return c.fetchReleaseStorageMetadata(ctx, clientset, driver, namespace, selector)
}

// GetReleaseValues returns the user-supplied values for the latest revision of
// the named release. It reinitialises the action configuration to the target
// namespace (same pattern as ListReleases) and restores it afterwards.
func (c *Client) GetReleaseValues(
	ctx context.Context,
	releaseName, namespace string,
) (map[string]any, error) {
	if releaseName == "" {
		return nil, errReleaseNameRequired
	}

	if c.actionConfig == nil || c.actionConfig.Releases == nil {
		return nil, errGetReleaseValuesUnsupported
	}

	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("get release values context cancelled: %w", err)
	}

	cleanup, err := c.switchNamespace(namespace)
	if err != nil {
		return nil, err
	}

	defer cleanup()

	getValues := helmv4action.NewGetValues(c.actionConfig)

	values, err := getValues.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("get release values for %s/%s: %w", namespace, releaseName, err)
	}

	return values, nil
}

type releaseStorageItem struct {
	Labels map[string]string
	UID    string
}

// fetchReleaseStorageMetadata queries either ConfigMaps or Secrets based on
// the configured Helm storage driver.
func (c *Client) fetchReleaseStorageMetadata(
	ctx context.Context,
	clientset kubernetes.Interface,
	driver, namespace, selector string,
) (*ReleaseStorageMetadata, error) {
	err := ValidateKubernetesReleaseStorageDriver(driver)
	if err != nil {
		return nil, err
	}

	var items []releaseStorageItem

	switch driver {
	case "configmap", "configmaps":
		cmList, err := clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return nil, fmt.Errorf(
				"list helm release configmaps in namespace %q: %w", namespace, err,
			)
		}

		for i := range cmList.Items {
			items = append(items, releaseStorageItem{
				Labels: cmList.Items[i].Labels,
				UID:    string(cmList.Items[i].UID),
			})
		}
	default:
		secretList, err := clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return nil, fmt.Errorf(
				"list helm release secrets in namespace %q: %w", namespace, err,
			)
		}

		for i := range secretList.Items {
			items = append(items, releaseStorageItem{
				Labels: secretList.Items[i].Labels,
				UID:    string(secretList.Items[i].UID),
			})
		}
	}

	return latestReleaseStorageMetadata(items)
}

func latestReleaseStorageMetadata(
	items []releaseStorageItem,
) (*ReleaseStorageMetadata, error) {
	if len(items) == 0 {
		return nil, ErrNoReleaseStorage
	}

	bestIdx := 0
	bestVersion := -1

	for i, item := range items {
		v, _ := strconv.Atoi(item.Labels["version"])
		if v > bestVersion {
			bestIdx = i
			bestVersion = v
		}
	}

	return &ReleaseStorageMetadata{
		Labels:            items[bestIdx].Labels,
		Identity:          items[bestIdx].UID,
		HistoryIdentities: releaseStorageIdentities(items),
	}, nil
}

func releaseStorageIdentities(items []releaseStorageItem) []string {
	identities := make([]string, 0, len(items))
	for _, item := range items {
		if identity := strings.TrimSpace(item.UID); identity != "" {
			identities = append(identities, identity)
		}
	}

	return identities
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

	chart, vals, err := c.loadChartAndValues(ctx, spec, client)
	if err != nil {
		return nil, err
	}

	runFn := func() (any, error) {
		return client.RunWithContext(ctx, chart, vals)
	}

	return executeAndExtractRelease(runFn)
}

func (c *Client) upgradeRelease(ctx context.Context, spec *ChartSpec) (*v1.Release, error) {
	client := helmv4action.NewUpgrade(c.actionConfig)
	client.Namespace = spec.Namespace

	applyCommonActionConfig(upgradeActionAdapter{client}, spec)

	// Note: Atomic is not supported in Helm v4 Upgrade action
	client.SkipCRDs = !spec.UpgradeCRDs // Inverted logic in v4

	chart, vals, err := c.loadChartAndValues(ctx, spec, client)
	if err != nil {
		return nil, err
	}

	runFn := func() (any, error) {
		return client.RunWithContext(ctx, spec.ReleaseName, chart, vals)
	}

	return executeAndExtractRelease(runFn)
}
