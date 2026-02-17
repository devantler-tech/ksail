package helmutil

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
)

// Base provides standard Helm chart lifecycle management for repository-based
// installers. It implements the installer.Installer interface (Install,
// Uninstall, Images) by managing a single chart from a named Helm repository.
//
// Embed *Base in installer types that follow the pattern of adding a
// repository, installing/upgrading a chart, uninstalling a release, and
// listing images from the chart manifest.
type Base struct {
	name    string
	client  helm.Interface
	timeout time.Duration
	repo    *helm.RepositoryEntry
	spec    *helm.ChartSpec
}

// NewBase creates a new Base with the given configuration. The name parameter
// is used in error messages to identify the component (e.g. "kyverno",
// "cert-manager").
func NewBase(
	name string,
	client helm.Interface,
	timeout time.Duration,
	repo *helm.RepositoryEntry,
	spec *helm.ChartSpec,
) *Base {
	return &Base{
		name:    name,
		client:  client,
		timeout: timeout,
		repo:    repo,
		spec:    spec,
	}
}

// Install adds the Helm repository and installs or upgrades the chart.
// It wraps the context with a deadline that includes [helm.ContextTimeoutBuffer]
// beyond the chart timeout so that Helm's internal kstatus watchers can
// observe resource readiness and report errors before the Go context expires.
func (b *Base) Install(ctx context.Context) error {
	err := b.client.AddRepository(ctx, b.repo, b.timeout)
	if err != nil {
		return fmt.Errorf("failed to add %s repository: %w", b.repo.Name, err)
	}

	installCtx, cancel := context.WithTimeout(ctx, b.timeout+helm.ContextTimeoutBuffer)
	defer cancel()

	err = helm.InstallChartWithRetry(installCtx, b.client, b.spec, b.name)
	if err != nil {
		return fmt.Errorf("installing %s chart: %w", b.name, err)
	}

	return nil
}

// Uninstall removes the Helm release.
func (b *Base) Uninstall(ctx context.Context) error {
	err := b.client.UninstallRelease(ctx, b.spec.ReleaseName, b.spec.Namespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall %s release: %w", b.name, err)
	}

	return nil
}

// Images returns the container images used by the chart by templating the
// chart and extracting image references from the rendered manifests.
func (b *Base) Images(ctx context.Context) ([]string, error) {
	return ImagesFromChart(ctx, b.client, b.spec)
}
