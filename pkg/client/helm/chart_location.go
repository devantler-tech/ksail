package helm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	helmv4action "helm.sh/helm/v4/pkg/action"
	helmv4loader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	helmv4cli "helm.sh/helm/v4/pkg/cli"
	helmv4registry "helm.sh/helm/v4/pkg/registry"
)

const chartRefParts = 2

const (
	// Retry configuration for chart acquisition on the render/template path.
	//
	// LocateChart downloads the repo index + chart archive (HTTP repos) or pulls
	// the OCI blob, populating the shared Helm cache. When the cache is cold a
	// transient 429/5xx/connection blip otherwise fails the locate outright, which
	// leaves the HelmRelease un-rendered (a render Degradation): its children are
	// dropped from the manifest stream, so `workload scan`/`validate` produce a
	// different resource set — and thus a different compliance score — than a run
	// against a warm cache (devantler-tech/ksail#5371). The install path already
	// retries acquisition via InstallChartWithRetry; the render path did not, so
	// mirror that here with the same transient-error policy.
	chartLocateMaxRetries    = 5
	chartLocateRetryBaseWait = 3 * time.Second
	chartLocateRetryMaxWait  = 30 * time.Second
)

// locateChartWithRetry runs a chart-locate operation, retrying on transient
// network errors (429/5xx, connection resets, timeouts, unexpected EOF) with
// capped exponential backoff. It mirrors InstallChartWithRetry for the
// render/template path so a cold-cache fetch blip does not silently degrade a
// render and make scan/validate output non-deterministic (ksail#5371).
//
// The retry runs while chartAcquireMu is held (see loadChartAndValues): the
// locate mutates the shared on-disk cache and must stay serialized, so the rare
// backoff briefly delays other parallel renders rather than letting them race a
// half-written cache. attempts/baseWait/maxWait are parameters so tests can
// drive the retry with millisecond waits.
func locateChartWithRetry(
	ctx context.Context,
	attempts int,
	baseWait, maxWait time.Duration,
	locate func() (string, error),
) (string, error) {
	var chartPath string

	err := netretry.Do(
		ctx,
		attempts,
		baseWait,
		maxWait,
		func() error {
			var locErr error

			chartPath, locErr = locate()

			return locErr
		},
	)
	if err != nil {
		return "", err //nolint:wrapcheck // caller wraps with chart-specific context
	}

	return chartPath, nil
}

// chartAcquireMu serializes the whole chart-acquisition window — locate →
// download → load → values-file reads — across the process, together with the
// process-global HELM_HTTP_TIMEOUT mutation in locateChartFromRepo.
//
// Every Helm client built by NewTemplateOnlyClient shares the SAME process-global
// Helm directories (HELM_REPOSITORY_CACHE, HELM_CONTENT_CACHE, the registry
// config), because helmv4cli.New() resolves them from the environment / user home
// rather than per-client. validate and scan render HelmReleases across
// kustomizations concurrently (pkg/cli/cmd/workload, validationConcurrency), so
// without serialization two renders acquire charts at once and race on those
// shared files: one download writes a chart archive / OCI blob / repo index while
// another reads or rewrites the same path. That silently corrupts the loaded
// chart and yields malformed, non-deterministic render output — the
// HelmRelease-dense overlays fail at a different place on every run
// (devantler-tech/ksail#5362). A `-race` build does not catch it because the race
// is on the filesystem, not Go memory (the prior env-var-only fix passed `-race`
// yet the corruption persisted in production).
//
// The lock is held by loadChartAndValues for acquisition only; rendering
// (RunWithContext, operating on the already-loaded in-memory chart) runs outside
// it and stays parallel.
//
//nolint:gochecknoglobals // package-level lock guarding process-global Helm state
var chartAcquireMu sync.Mutex

// chartLocator abstracts the common chart location capabilities shared by
// helmv4action.Install and helmv4action.Upgrade.
type chartLocator interface {
	SetRegistryClient(client *helmv4registry.Client)
	LocateChart(name string, settings *helmv4cli.EnvSettings) (string, error)
}

func (c *Client) loadChartAndValues(
	ctx context.Context,
	spec *ChartSpec,
	client any,
) (*chartv2.Chart, map[string]any, error) {
	// Serialize the entire acquisition window so concurrent renders never touch the
	// shared process-global Helm cache/config at the same time. See chartAcquireMu.
	chartAcquireMu.Lock()
	defer chartAcquireMu.Unlock()

	chartPath, chart, err := c.locateAndLoadChart(ctx, spec, client)
	if err != nil {
		return nil, nil, err
	}

	vals, err := c.mergeValues(spec, chartPath)
	if err != nil {
		return nil, nil, err
	}

	return chart, vals, nil
}

func (c *Client) locateAndLoadChart(
	ctx context.Context,
	spec *ChartSpec,
	client any,
) (string, *chartv2.Chart, error) {
	var chartPath string

	var err error

	switch {
	case spec.RepoURL != "":
		chartPath, err = c.locateChartFromRepo(ctx, spec, client)
	case helmv4registry.IsOCI(spec.ChartName):
		chartPath, err = c.locateOCIChart(ctx, spec, client)
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

// applyChartPathOptions applies ChartPathOptions to an Install or Upgrade client.
func applyChartPathOptions(client any, opts helmv4action.ChartPathOptions) {
	switch cl := client.(type) {
	case *helmv4action.Install:
		cl.ChartPathOptions = opts
	case *helmv4action.Upgrade:
		cl.ChartPathOptions = opts
	}
}

func (c *Client) locateOCIChart(ctx context.Context, spec *ChartSpec, client any) (string, error) {
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

	chartPath, err := locateChartWithRetry(
		ctx,
		chartLocateMaxRetries,
		chartLocateRetryBaseWait,
		chartLocateRetryMaxWait,
		func() (string, error) { return locator.LocateChart(spec.ChartName, c.settings) },
	)
	if err != nil {
		return "", fmt.Errorf("failed to locate OCI chart %q: %w", spec.ChartName, err)
	}

	return chartPath, nil
}

func (c *Client) locateChartFromRepo(
	ctx context.Context,
	spec *ChartSpec,
	client any,
) (string, error) {
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

	// chartAcquireMu (held by loadChartAndValues for the whole acquisition window)
	// already serializes this call, so the process-global HELM_HTTP_TIMEOUT
	// mutation below is race-free. The deferred restore runs when this function
	// returns — still within loadChartAndValues, lock held — so no other render
	// observes the temporary timeout.
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

	// Use LocateChart to download the chart to cache and return the local path,
	// retrying transient network failures so a cold-cache blip does not silently
	// degrade the render (ksail#5371).
	chartPath, err := locateChartWithRetry(
		ctx,
		chartLocateMaxRetries,
		chartLocateRetryBaseWait,
		chartLocateRetryMaxWait,
		func() (string, error) { return opts.LocateChart(chartName, c.settings) },
	)
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

func parseChartRef(chartRef string) (string, string) {
	parts := strings.SplitN(chartRef, "/", chartRefParts)
	if len(parts) == 1 {
		return "", parts[0]
	}

	return parts[0], parts[1]
}
