package argocdinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
)

const (
	argoCDReleaseName = "argocd"
	argoCDNamespace   = "argocd"
	argoCDChartName   = "oci://ghcr.io/argoproj/argo-helm/argo-cd"
)

// ArgoCDInstaller installs or upgrades Argo CD via its Helm OCI chart.
//
// It implements installer.Installer semantics (Install/Uninstall) so it can be
// orchestrated by cluster lifecycle flows.
type ArgoCDInstaller struct {
	timeout time.Duration
	client  helm.Interface
}

// NewArgoCDInstaller creates a new Argo CD installer instance.
func NewArgoCDInstaller(client helm.Interface, timeout time.Duration) *ArgoCDInstaller {
	return &ArgoCDInstaller{client: client, timeout: timeout}
}

// Install installs or upgrades Argo CD via its Helm chart.
func (a *ArgoCDInstaller) Install(ctx context.Context) error {
	err := a.helmInstallOrUpgradeArgoCD(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Argo CD: %w", err)
	}

	return nil
}

// Uninstall removes the Helm release for Argo CD.
func (a *ArgoCDInstaller) Uninstall(ctx context.Context) error {
	err := a.client.UninstallRelease(ctx, argoCDReleaseName, argoCDNamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall Argo CD release: %w", err)
	}

	return nil
}

// --- internals ---

func (a *ArgoCDInstaller) helmInstallOrUpgradeArgoCD(ctx context.Context) error {
	spec := &helm.ChartSpec{
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

	// Set context deadline longer than Helm timeout to ensure Helm has
	// sufficient time to complete its kstatus-based wait operation.
	// Add 5 minutes buffer to the Helm timeout.
	contextTimeout := a.timeout + (5 * time.Minute)
	timeoutCtx, cancel := context.WithTimeout(ctx, contextTimeout)
	defer cancel()

	_, err := a.client.InstallOrUpgradeChart(timeoutCtx, spec)
	if err != nil {
		return fmt.Errorf("failed to install Argo CD chart: %w", err)
	}

	return nil
}
