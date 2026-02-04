package fluxinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
)

// FluxInstaller implements the installer.Installer interface for Flux.
type FluxInstaller struct {
	timeout time.Duration
	client  helm.Interface
}

// NewFluxInstaller creates a new Flux installer instance.
func NewFluxInstaller(
	client helm.Interface,
	timeout time.Duration,
) *FluxInstaller {
	return &FluxInstaller{
		client:  client,
		timeout: timeout,
	}
}

// Install installs or upgrades the Flux Operator via its OCI Helm chart.
func (b *FluxInstaller) Install(ctx context.Context) error {
	err := b.helmInstallOrUpgradeFluxOperator(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Flux operator: %w", err)
	}

	return nil
}

// Uninstall removes the Helm release for the Flux Operator.
func (b *FluxInstaller) Uninstall(ctx context.Context) error {
	err := b.client.UninstallRelease(ctx, "flux-operator", "flux-system")
	if err != nil {
		return fmt.Errorf("failed to uninstall flux-operator release: %w", err)
	}

	return nil
}

// Images returns the container images used by the Flux Operator.
func (b *FluxInstaller) Images(ctx context.Context) ([]string, error) {
	spec := b.chartSpec()

	manifest, err := b.client.TemplateChart(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to template flux-operator chart: %w", err)
	}

	return image.ExtractImagesFromManifest(manifest)
}

func (b *FluxInstaller) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName:     "flux-operator",
		ChartName:       "oci://ghcr.io/controlplaneio-fluxcd/charts/flux-operator",
		Namespace:       "flux-system",
		CreateNamespace: true,
		Atomic:          true,
		UpgradeCRDs:     true,
		Timeout:         b.timeout,
		Wait:            true,
		WaitForJobs:     true,
		// Silence Helm stderr because the Flux operator CRDs emit harmless
		// "unrecognized format" warnings that confuse users if printed.
		Silent: true,
	}
}

// --- internals ---

func (b *FluxInstaller) helmInstallOrUpgradeFluxOperator(ctx context.Context) error {
	spec := b.chartSpec()

	// Set context deadline longer than Helm timeout to ensure Helm has
	// sufficient time to complete its kstatus-based wait operation.
	// Add 5 minutes buffer to the Helm timeout.
	//
	// Note: This installer calls client.InstallOrUpgradeChart directly (not the
	// helm.InstallOrUpgradeChart helper) because OCI charts don't require repository
	// registration. Therefore, we must apply the context timeout buffer here.
	contextTimeout := b.timeout + helm.ContextTimeoutBuffer

	timeoutCtx, cancel := context.WithTimeout(ctx, contextTimeout)
	defer cancel()

	_, err := b.client.InstallOrUpgradeChart(timeoutCtx, spec)
	if err != nil {
		return fmt.Errorf("failed to install flux operator chart: %w", err)
	}

	return nil
}
