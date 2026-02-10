package gatekeeperinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
)

const (
	gatekeeperRepoName  = "gatekeeper"
	gatekeeperRepoURL   = "https://open-policy-agent.github.io/gatekeeper/charts"
	gatekeeperRelease   = "gatekeeper"
	gatekeeperNamespace = "gatekeeper-system"
	gatekeeperChartName = "gatekeeper/gatekeeper"
)

// GatekeeperInstaller installs or upgrades Gatekeeper.
//
// It implements installer.Installer semantics (Install/Uninstall) so it can be
// orchestrated by cluster lifecycle flows.
type GatekeeperInstaller struct {
	client  helm.Interface
	timeout time.Duration
}

// NewGatekeeperInstaller creates a new Gatekeeper installer instance.
func NewGatekeeperInstaller(client helm.Interface, timeout time.Duration) *GatekeeperInstaller {
	return &GatekeeperInstaller{client: client, timeout: timeout}
}

// Install installs or upgrades Gatekeeper via its Helm chart.
func (g *GatekeeperInstaller) Install(ctx context.Context) error {
	return g.helmInstallOrUpgradeGatekeeper(ctx)
}

// Uninstall removes the Helm release for Gatekeeper.
func (g *GatekeeperInstaller) Uninstall(ctx context.Context) error {
	err := g.client.UninstallRelease(ctx, gatekeeperRelease, gatekeeperNamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall gatekeeper release: %w", err)
	}

	return nil
}

// Images returns the container images used by Gatekeeper.
func (g *GatekeeperInstaller) Images(ctx context.Context) ([]string, error) {
	spec := g.chartSpec()

	manifest, err := g.client.TemplateChart(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to template gatekeeper chart: %w", err)
	}

	images, err := image.ExtractImagesFromManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("extract images from gatekeeper manifest: %w", err)
	}

	return images, nil
}

func (g *GatekeeperInstaller) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName:     gatekeeperRelease,
		ChartName:       gatekeeperChartName,
		Namespace:       gatekeeperNamespace,
		RepoURL:         gatekeeperRepoURL,
		CreateNamespace: true,
		Atomic:          true,
		Wait:            true,
		WaitForJobs:     true,
		Timeout:         g.timeout,
		SetValues: map[string]string{
			// Use Ignore so the validating webhook does not block API
			// requests when webhook pods are temporarily unreachable
			// (e.g. during CNI churn on freshly bootstrapped clusters).
			"webhook.failurePolicy": "Ignore",
		},
	}
}

func (g *GatekeeperInstaller) helmInstallOrUpgradeGatekeeper(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{Name: gatekeeperRepoName, URL: gatekeeperRepoURL}

	err := g.client.AddRepository(ctx, repoEntry, g.timeout)
	if err != nil {
		return fmt.Errorf("failed to add gatekeeper repository: %w", err)
	}

	spec := g.chartSpec()

	_, err = g.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install gatekeeper chart: %w", err)
	}

	return nil
}
