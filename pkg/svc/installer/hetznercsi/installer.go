package hetznercsiinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/hetzner"
)

// ErrHetznerTokenNotSet is returned when the HCLOUD_TOKEN environment variable is not set.
var ErrHetznerTokenNotSet = hetzner.ErrTokenNotSet

// Installer installs or upgrades the Hetzner Cloud CSI driver.
//
// It embeds helmutil.Base for the Helm lifecycle and adds a pre-install step
// that creates the required Kubernetes secret with the Hetzner Cloud API token.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
type Installer struct {
	*helmutil.Base

	kubeconfig string
	context    string
}

// NewInstaller creates a new Hetzner CSI installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"hetzner-csi",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "hcloud",
				URL:  "https://charts.hetzner.cloud",
			},
			&helm.ChartSpec{
				ReleaseName:     "hcloud-csi",
				ChartName:       "hcloud/hcloud-csi",
				Namespace:       hetzner.Namespace,
				RepoURL:         "https://charts.hetzner.cloud",
				CreateNamespace: false, // kube-system already exists
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				Timeout:         timeout,
			},
		),
		kubeconfig: kubeconfig,
		context:    context,
	}
}

// Install creates the required Hetzner Cloud API token secret and then
// installs or upgrades the CSI driver via its Helm chart.
func (h *Installer) Install(ctx context.Context) error {
	err := hetzner.InstallWithSecret(ctx, h.Base, h.kubeconfig, h.context, "hetzner-csi")
	if err != nil {
		return fmt.Errorf("install hetzner-csi: %w", err)
	}

	return nil
}
