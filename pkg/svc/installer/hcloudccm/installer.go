package hcloudccminstaller

import (
	"strings"
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
//
// When networkName is set, the network name is stored in the shared "hcloud"
// Kubernetes secret (key "network") so the chart's default
// valueFrom.secretKeyRef can read it as HCLOUD_NETWORK.
//
// When haEnabled is true the chart is configured with replicaCount=2
// for fast failover via leader election.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	networkName string,
	haEnabled bool,
) *Installer {
	return hetzner.NewInstaller(client, kubeconfig, context, timeout, hetzner.ChartConfig{
		Name:        "hcloud-ccm",
		ReleaseName: "hcloud-cloud-controller-manager",
		ChartName:   "hcloud/hcloud-cloud-controller-manager",
		Version:     chartVersion(),
		ValuesYaml:  buildValuesYaml(networkName, haEnabled),
		SecretData:  buildSecretData(networkName),
	})
}

// buildSecretData returns extra key-value pairs for the shared "hcloud" secret.
// When networkName is set, it includes the "network" key so the chart's default
// valueFrom.secretKeyRef reads it as HCLOUD_NETWORK.
func buildSecretData(networkName string) map[string][]byte {
	if networkName == "" {
		return nil
	}

	return map[string][]byte{
		"network": []byte(networkName),
	}
}

// buildValuesYaml generates the Helm values YAML for the hcloud-ccm chart.
// When networkName is set, it enables the chart's networking section so that
// HCLOUD_NETWORK is injected into the CCM pod. The network name itself is
// stored in the "hcloud" Kubernetes secret (key "network") and read by the
// chart's default valueFrom.secretKeyRef — no inline value override is needed.
// When haEnabled is true an extra standby replica is configured.
func buildValuesYaml(networkName string, haEnabled bool) string {
	var parts []string

	if networkName != "" {
		parts = append(parts, "networking:\n  enabled: true\n  clusterCIDR: "+DefaultClusterCIDR)
	}

	if haEnabled {
		parts = append(parts, "replicaCount: 2")
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "\n")
}
