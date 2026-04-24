package hcloudccminstaller

import (
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"
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
// It also initializes nodes by matching Kubernetes nodes to Hetzner Cloud servers
// using private network IPs (requires HCLOUD_NETWORK to be set).
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
//   - The token requires read/write access to Load Balancers
type Installer = hetzner.Installer

// DefaultClusterCIDR is the default pod CIDR for Kubernetes clusters.
// This matches the Talos/Kubernetes default and is required by Cilium
// in ipam.mode=kubernetes for node pod CIDR allocation.
const DefaultClusterCIDR = "10.244.0.0/16"

// NewInstaller creates a new Hetzner Cloud Controller Manager installer instance.
// The networkName parameter specifies the Hetzner Cloud private network name
// that CCM uses to look up servers by their private IPs. If empty, networking
// support is not enabled in the CCM chart values.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	networkName string,
) *Installer {
	return hetzner.NewInstaller(client, kubeconfig, context, timeout, hetzner.ChartConfig{
		Name:        "hcloud-ccm",
		ReleaseName: "hcloud-cloud-controller-manager",
		ChartName:   "hcloud/hcloud-cloud-controller-manager",
		Version:     chartVersion(),
		ValuesYaml:  buildValuesYaml(networkName),
	})
}

// buildValuesYaml generates the Helm values YAML for the hcloud-ccm chart.
// When networkName is set, it enables the chart's networking section so that
// HCLOUD_NETWORK is injected into the CCM pod, allowing it to match Kubernetes
// nodes to Hetzner Cloud servers by their private network IPs.
func buildValuesYaml(networkName string) string {
	if networkName == "" {
		return ""
	}

	return fmt.Sprintf(`networking:
  enabled: true
  clusterCIDR: %s
  network:
    value: %q`, DefaultClusterCIDR, networkName)
}
