package hetznercsiinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"
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
//
// The networkName parameter specifies the Hetzner Cloud private network name.
// When set, it is stored in the shared "hcloud" Kubernetes secret (key "network")
// alongside the HCLOUD_TOKEN. Storing the network name symmetrically from both
// the CCM and CSI installers ensures the secret is always populated with the
// correct network regardless of which component installs first (or alone),
// which is required by GitOps-managed CCM HelmReleases that read HCLOUD_NETWORK
// from this secret via the chart's default valueFrom.secretKeyRef.
//
// An empty networkName leaves the "network" key untouched so concurrent
// installers (e.g. CCM) don't overwrite each other's values.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	networkName string,
) *Installer {
	return hetzner.NewInstaller(client, kubeconfig, context, timeout, hetzner.ChartConfig{
		Name:        "hetzner-csi",
		ReleaseName: "hcloud-csi",
		ChartName:   "hcloud/hcloud-csi",
		Version:     chartVersion(),
		SecretData:  buildSecretData(networkName),
	})
}

// buildSecretData returns extra key-value pairs for the shared "hcloud" secret.
// When networkName is set, it includes the "network" key so that consumers of
// the secret (the hcloud-ccm chart's default valueFrom.secretKeyRef, and any
// GitOps-managed CCM/CSI HelmReleases) can read HCLOUD_NETWORK from it.
func buildSecretData(networkName string) map[string][]byte {
	if networkName == "" {
		return nil
	}

	return map[string][]byte{
		"network": []byte(networkName),
	}
}
