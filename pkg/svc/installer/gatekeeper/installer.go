// Package gatekeeperinstaller installs Gatekeeper via Helm.
package gatekeeperinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
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

func (g *GatekeeperInstaller) helmInstallOrUpgradeGatekeeper(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{Name: gatekeeperRepoName, URL: gatekeeperRepoURL}

	err := g.client.AddRepository(ctx, repoEntry, g.timeout)
	if err != nil {
		return fmt.Errorf("failed to add gatekeeper repository: %w", err)
	}

	spec := &helm.ChartSpec{
		ReleaseName:     gatekeeperRelease,
		ChartName:       gatekeeperChartName,
		Namespace:       gatekeeperNamespace,
		RepoURL:         gatekeeperRepoURL,
		CreateNamespace: true,
		Atomic:          true,
		Wait:            true,
		WaitForJobs:     true,
		Timeout:         g.timeout,
	}

	_, err = g.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install gatekeeper chart: %w", err)
	}

	return nil
}
