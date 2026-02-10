package certmanagerinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
)

const (
	certManagerRepoName  = "jetstack"
	certManagerRepoURL   = "https://charts.jetstack.io"
	certManagerRelease   = "cert-manager"
	certManagerNamespace = "cert-manager"
	certManagerChartName = "jetstack/cert-manager"
)

// CertManagerInstaller installs or upgrades cert-manager.
//
// It implements installer.Installer semantics (Install/Uninstall) so it can be
// orchestrated by cluster lifecycle flows.
type CertManagerInstaller struct {
	client  helm.Interface
	timeout time.Duration
}

// NewCertManagerInstaller creates a new cert-manager installer instance.
func NewCertManagerInstaller(client helm.Interface, timeout time.Duration) *CertManagerInstaller {
	return &CertManagerInstaller{client: client, timeout: timeout}
}

// Install installs or upgrades cert-manager via its Helm chart.
func (c *CertManagerInstaller) Install(ctx context.Context) error {
	return c.helmInstallOrUpgradeCertManager(ctx)
}

// Uninstall removes the Helm release for cert-manager.
func (c *CertManagerInstaller) Uninstall(ctx context.Context) error {
	err := c.client.UninstallRelease(ctx, certManagerRelease, certManagerNamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall cert-manager release: %w", err)
	}

	return nil
}

// Images returns the container images used by cert-manager.
func (c *CertManagerInstaller) Images(ctx context.Context) ([]string, error) {
	spec := c.chartSpec()

	manifest, err := c.client.TemplateChart(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to template cert-manager chart: %w", err)
	}

	images, err := image.ExtractImagesFromManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("extract images from cert-manager manifest: %w", err)
	}

	return images, nil
}

func (c *CertManagerInstaller) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName:     certManagerRelease,
		ChartName:       certManagerChartName,
		Namespace:       certManagerNamespace,
		RepoURL:         certManagerRepoURL,
		CreateNamespace: true,
		Atomic:          true,
		Wait:            true,
		WaitForJobs:     true,
		Timeout:         c.timeout,
		SetValues: map[string]string{
			"installCRDs":             "true",
			"startupapicheck.timeout": "5m",
		},
	}
}

func (c *CertManagerInstaller) helmInstallOrUpgradeCertManager(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{Name: certManagerRepoName, URL: certManagerRepoURL}

	err := c.client.AddRepository(ctx, repoEntry, c.timeout)
	if err != nil {
		return fmt.Errorf("failed to add jetstack repository: %w", err)
	}

	spec := c.chartSpec()

	_, err = c.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install cert-manager chart: %w", err)
	}

	return nil
}
