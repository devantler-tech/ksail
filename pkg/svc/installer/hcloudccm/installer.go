package hcloudccminstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/hetzner"
)

// ErrHetznerTokenNotSet is returned when the HCLOUD_TOKEN environment variable is not set.
var ErrHetznerTokenNotSet = hetzner.ErrTokenNotSet

// Installer installs or upgrades the Hetzner Cloud Controller Manager.
//
// It delegates to hetzner.Installer which handles the shared Hetzner lifecycle:
// creating the HCLOUD_TOKEN secret and installing the Helm chart.
//
// The cloud controller manager enables LoadBalancer services on Hetzner Cloud
// by provisioning Hetzner Load Balancers and managing their lifecycle.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
//   - The token requires read/write access to Load Balancers
type Installer = hetzner.Installer

// NewInstaller creates a new Hetzner Cloud Controller Manager installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *Installer {
	return hetzner.NewInstaller(client, kubeconfig, context, timeout, hetzner.ChartConfig{
		Name:        "hcloud-ccm",
		ReleaseName: "hcloud-cloud-controller-manager",
		ChartName:   "hcloud/hcloud-cloud-controller-manager",
		Version:     chartVersion(),
	})
}
