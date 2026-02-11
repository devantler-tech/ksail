package argocdinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
)

const (
	argoCDReleaseName = "argocd"
	argoCDNamespace   = "argocd"
	argoCDChartName   = "oci://ghcr.io/argoproj/argo-helm/argo-cd"
)

// Installer installs or upgrades Argo CD via its Helm OCI chart.
//
// It implements installer.Installer semantics (Install/Uninstall) so it can be
// orchestrated by cluster lifecycle flows.
type Installer struct {
	timeout time.Duration
	client  helm.Interface
}

// NewInstaller creates a new Argo CD installer instance.
func NewInstaller(client helm.Interface, timeout time.Duration) *Installer {
	return &Installer{client: client, timeout: timeout}
}

// Install installs or upgrades Argo CD via its Helm chart.
func (a *Installer) Install(ctx context.Context) error {
	err := a.helmInstallOrUpgrade(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Argo CD: %w", err)
	}

	return nil
}

// Uninstall removes the Helm release for Argo CD.
func (a *Installer) Uninstall(ctx context.Context) error {
	err := a.client.UninstallRelease(ctx, argoCDReleaseName, argoCDNamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall Argo CD release: %w", err)
	}

	return nil
}

// Images returns the container images used by Argo CD.
func (a *Installer) Images(ctx context.Context) ([]string, error) {
	images, err := helmutil.ImagesFromChart(ctx, a.client, a.chartSpec())
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}

	return images, nil
}

func (a *Installer) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName:     argoCDReleaseName,
		ChartName:       argoCDChartName,
		Namespace:       argoCDNamespace,
		CreateNamespace: true,
		Atomic:          true,
		UpgradeCRDs:     true,
		Timeout:         a.timeout,
		Wait:            true,
		WaitForJobs:     true,
	}
}

// --- internals ---

func (a *Installer) helmInstallOrUpgrade(ctx context.Context) error {
	spec := a.chartSpec()

	// Set context deadline longer than Helm timeout to ensure Helm has
	// sufficient time to complete its kstatus-based wait operation.
	// Add 5 minutes buffer to the Helm timeout.
	//
	// Note: This installer calls client.InstallOrUpgradeChart directly (not the
	// helm.InstallOrUpgradeChart helper) because OCI charts don't require repository
	// registration. Therefore, we must apply the context timeout buffer here.
	contextTimeout := a.timeout + helm.ContextTimeoutBuffer

	timeoutCtx, cancel := context.WithTimeout(ctx, contextTimeout)
	defer cancel()

	_, err := a.client.InstallOrUpgradeChart(timeoutCtx, spec)
	if err != nil {
		return fmt.Errorf("failed to install Argo CD chart: %w", err)
	}

	return nil
}
