// Package kyvernoinstaller installs Kyverno via Helm.
package kyvernoinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
)

const (
	kyvernoRepoName  = "kyverno"
	kyvernoRepoURL   = "https://kyverno.github.io/kyverno/"
	kyvernoRelease   = "kyverno"
	kyvernoNamespace = "kyverno"
	kyvernoChartName = "kyverno/kyverno"
)

// KyvernoInstaller installs or upgrades Kyverno.
//
// It implements installer.Installer semantics (Install/Uninstall) so it can be
// orchestrated by cluster lifecycle flows.
type KyvernoInstaller struct {
	client  helm.Interface
	timeout time.Duration
}

// NewKyvernoInstaller creates a new Kyverno installer instance.
func NewKyvernoInstaller(client helm.Interface, timeout time.Duration) *KyvernoInstaller {
	return &KyvernoInstaller{client: client, timeout: timeout}
}

// Install installs or upgrades Kyverno via its Helm chart.
func (k *KyvernoInstaller) Install(ctx context.Context) error {
	return k.helmInstallOrUpgradeKyverno(ctx)
}

// Uninstall removes the Helm release for Kyverno.
func (k *KyvernoInstaller) Uninstall(ctx context.Context) error {
	err := k.client.UninstallRelease(ctx, kyvernoRelease, kyvernoNamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall kyverno release: %w", err)
	}

	return nil
}

func (k *KyvernoInstaller) helmInstallOrUpgradeKyverno(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{Name: kyvernoRepoName, URL: kyvernoRepoURL}

	err := k.client.AddRepository(ctx, repoEntry, k.timeout)
	if err != nil {
		return fmt.Errorf("failed to add kyverno repository: %w", err)
	}

	spec := &helm.ChartSpec{
		ReleaseName:     kyvernoRelease,
		ChartName:       kyvernoChartName,
		Namespace:       kyvernoNamespace,
		RepoURL:         kyvernoRepoURL,
		CreateNamespace: true,
		Atomic:          true,
		Wait:            true,
		WaitForJobs:     true,
		Timeout:         k.timeout,
	}

	_, err = k.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install kyverno chart: %w", err)
	}

	return nil
}
