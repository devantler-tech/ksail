package kyvernoinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
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

// Images returns the container images used by Kyverno.
func (k *KyvernoInstaller) Images(ctx context.Context) ([]string, error) {
	spec := k.chartSpec()

	manifest, err := k.client.TemplateChart(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to template kyverno chart: %w", err)
	}

	images, err := image.ExtractImagesFromManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("extract images from kyverno manifest: %w", err)
	}

	return images, nil
}

func (k *KyvernoInstaller) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
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
}

func (k *KyvernoInstaller) helmInstallOrUpgradeKyverno(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{Name: kyvernoRepoName, URL: kyvernoRepoURL}

	err := k.client.AddRepository(ctx, repoEntry, k.timeout)
	if err != nil {
		return fmt.Errorf("failed to add kyverno repository: %w", err)
	}

	spec := k.chartSpec()

	_, err = k.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install kyverno chart: %w", err)
	}

	return nil
}
