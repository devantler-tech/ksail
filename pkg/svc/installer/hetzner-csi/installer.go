package hetznercsiinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
)

const (
	hetznerCSIRepoName  = "hcloud-csi"
	hetznerCSIRepoURL   = "https://charts.hetzner.cloud"
	hetznerCSIRelease   = "hcloud-csi"
	hetznerCSINamespace = "kube-system"
	hetznerCSIChartName = "hcloud-csi/hcloud-csi"
)

// HetznerCSIInstaller installs or upgrades the Hetzner Cloud CSI driver.
//
// It implements installer.Installer semantics (Install/Uninstall) so it can be
// orchestrated by cluster lifecycle flows.
type HetznerCSIInstaller struct {
	client  helm.Interface
	timeout time.Duration
}

// NewHetznerCSIInstaller creates a new Hetzner CSI installer instance.
func NewHetznerCSIInstaller(client helm.Interface, timeout time.Duration) *HetznerCSIInstaller {
	return &HetznerCSIInstaller{client: client, timeout: timeout}
}

// Install installs or upgrades the Hetzner Cloud CSI driver via its Helm chart.
func (h *HetznerCSIInstaller) Install(ctx context.Context) error {
	return h.helmInstallOrUpgradeHetznerCSI(ctx)
}

// Uninstall removes the Helm release for the Hetzner CSI driver.
func (h *HetznerCSIInstaller) Uninstall(ctx context.Context) error {
	err := h.client.UninstallRelease(ctx, hetznerCSIRelease, hetznerCSINamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall hetzner-csi release: %w", err)
	}

	return nil
}

func (h *HetznerCSIInstaller) helmInstallOrUpgradeHetznerCSI(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{Name: hetznerCSIRepoName, URL: hetznerCSIRepoURL}

	err := h.client.AddRepository(ctx, repoEntry, h.timeout)
	if err != nil {
		return fmt.Errorf("failed to add hetzner CSI repository: %w", err)
	}

	spec := &helm.ChartSpec{
		ReleaseName:     hetznerCSIRelease,
		ChartName:       hetznerCSIChartName,
		Namespace:       hetznerCSINamespace,
		RepoURL:         hetznerCSIRepoURL,
		CreateNamespace: false, // kube-system already exists
		Atomic:          true,
		Wait:            true,
		WaitForJobs:     true,
		Timeout:         h.timeout,
	}

	_, err = h.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install hetzner-csi chart: %w", err)
	}

	return nil
}
