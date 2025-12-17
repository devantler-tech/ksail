package fluxinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail-go/pkg/client/helm"
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

// --- internals ---

func (b *FluxInstaller) helmInstallOrUpgradeFluxOperator(ctx context.Context) error {
	spec := &helm.ChartSpec{
		ReleaseName:     "flux-operator",
		ChartName:       "oci://ghcr.io/controlplaneio-fluxcd/charts/flux-operator",
		Namespace:       "flux-system",
		CreateNamespace: true,
		Atomic:          true,
		UpgradeCRDs:     true,
		Timeout:         b.timeout,
		// Silence Helm stderr because the Flux operator CRDs emit harmless
		// "unrecognized format" warnings that confuse users if printed.
		Silent: true,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	_, err := b.client.InstallOrUpgradeChart(timeoutCtx, spec)
	if err != nil {
		return fmt.Errorf("failed to install flux operator chart: %w", err)
	}

	return nil
}
