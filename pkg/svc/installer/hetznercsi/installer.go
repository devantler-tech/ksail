package hetznercsiinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/hetzner"
)

// ErrHetznerTokenNotSet is returned when the HCLOUD_TOKEN environment variable is not set.
var ErrHetznerTokenNotSet = hetzner.ErrTokenNotSet

// Installer installs or upgrades the Hetzner Cloud CSI driver.
//
// It delegates to hetzner.Installer which handles the shared Hetzner lifecycle:
// creating the HCLOUD_TOKEN secret and installing the Helm chart.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
type Installer = hetzner.Installer

// NewInstaller creates a new Hetzner CSI installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *Installer {
	return hetzner.NewInstaller(client, kubeconfig, context, timeout, hetzner.ChartConfig{
		Name:        "hetzner-csi",
		ReleaseName: "hcloud-csi",
		ChartName:   "hcloud/hcloud-csi",
		Version:     chartVersion(),
	})
}
