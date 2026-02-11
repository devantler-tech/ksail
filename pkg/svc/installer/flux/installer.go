package fluxinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
)

// Installer implements the installer.Installer interface for Flux.
type Installer struct {
	timeout time.Duration
	client  helm.Interface
}

// NewInstaller creates a new Flux installer instance.
func NewInstaller(
	client helm.Interface,
	timeout time.Duration,
) *Installer {
	return &Installer{
		client:  client,
		timeout: timeout,
	}
}

// Install installs or upgrades the Flux Operator via its OCI Helm chart.
func (b *Installer) Install(ctx context.Context) error {
	err := b.helmInstallOrUpgrade(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Flux operator: %w", err)
	}

	return nil
}

// Uninstall removes the Helm release for the Flux Operator.
func (b *Installer) Uninstall(ctx context.Context) error {
	err := b.client.UninstallRelease(ctx, "flux-operator", "flux-system")
	if err != nil {
		return fmt.Errorf("failed to uninstall flux-operator release: %w", err)
	}

	return nil
}

// Images returns the container images used by the Flux Operator.
func (b *Installer) Images(ctx context.Context) ([]string, error) {
	images, err := helmutil.ImagesFromChart(ctx, b.client, b.chartSpec())
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}

	return images, nil
}

func (b *Installer) chartSpec() *helm.ChartSpec {
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

func (b *Installer) helmInstallOrUpgrade(ctx context.Context) error {
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
