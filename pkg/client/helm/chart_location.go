package helm

import (
	"fmt"
	"os"
	"strings"

	helmv4action "helm.sh/helm/v4/pkg/action"
	helmv4loader "helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	helmv4cli "helm.sh/helm/v4/pkg/cli"
	helmv4registry "helm.sh/helm/v4/pkg/registry"
)

const chartRefParts = 2

// chartLocator abstracts the common chart location capabilities shared by
// helmv4action.Install and helmv4action.Upgrade.
type chartLocator interface {
	SetRegistryClient(client *helmv4registry.Client)
	LocateChart(name string, settings *helmv4cli.EnvSettings) (string, error)
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

func parseChartRef(chartRef string) (string, string) {
	parts := strings.SplitN(chartRef, "/", chartRefParts)
	if len(parts) == 1 {
		return "", parts[0]
	}

	return parts[0], parts[1]
}
