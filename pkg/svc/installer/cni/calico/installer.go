package calicoinstaller

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni"
)

// Distribution represents the Kubernetes distribution type.
type Distribution string

// Supported distribution types.
const (
	DistributionKind          Distribution = "kind"
	DistributionK3d           Distribution = "k3d"
	DistributionTalosInDocker Distribution = "talosindocker"
)

// CalicoInstaller implements the installer.Installer interface for Calico.
type CalicoInstaller struct {
	*cni.InstallerBase

	distribution Distribution
}

// NewCalicoInstaller creates a new Calico installer instance.
func NewCalicoInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *CalicoInstaller {
	return NewCalicoInstallerWithDistribution(client, kubeconfig, context, timeout, "")
}

// NewCalicoInstallerWithDistribution creates a new Calico installer with distribution-specific configuration.
func NewCalicoInstallerWithDistribution(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	distribution Distribution,
) *CalicoInstaller {
	calicoInstaller := &CalicoInstaller{
		distribution: distribution,
	}
	calicoInstaller.InstallerBase = cni.NewInstallerBase(
		client,
		kubeconfig,
		context,
		timeout,
		calicoInstaller.waitForReadiness,
	)

	return calicoInstaller
}

// Install installs or upgrades Calico via its Helm chart.
func (c *CalicoInstaller) Install(ctx context.Context) error {
	err := c.helmInstallOrUpgradeCalico(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Calico: %w", err)
	}

	return nil
}

// SetWaitForReadinessFunc overrides the readiness wait function. Primarily used for testing.
func (c *CalicoInstaller) SetWaitForReadinessFunc(waitFunc func(context.Context) error) {
	c.InstallerBase.SetWaitForReadinessFunc(waitFunc, c.waitForReadiness)
}

// Uninstall removes the Helm release for Calico.
func (c *CalicoInstaller) Uninstall(ctx context.Context) error {
	client, err := c.GetClient()
	if err != nil {
		return fmt.Errorf("get helm client: %w", err)
	}

	err = client.UninstallRelease(ctx, "calico", "tigera-operator")
	if err != nil {
		return fmt.Errorf("failed to uninstall calico release: %w", err)
	}

	return nil
}

// --- internals ---

func (c *CalicoInstaller) helmInstallOrUpgradeCalico(ctx context.Context) error {
	client, err := c.GetClient()
	if err != nil {
		return fmt.Errorf("get helm client: %w", err)
	}

	repoConfig := helm.RepoConfig{
		Name:     "projectcalico",
		URL:      "https://docs.tigera.io/calico/charts",
		RepoName: "calico",
	}

	chartConfig := helm.ChartConfig{
		ReleaseName:     "calico",
		ChartName:       "projectcalico/tigera-operator",
		Namespace:       "tigera-operator",
		RepoURL:         "https://docs.tigera.io/calico/charts",
		CreateNamespace: true,
		SetJSONVals:     c.getCalicoValues(),
	}

	err = helm.InstallOrUpgradeChart(ctx, client, repoConfig, chartConfig, c.GetTimeout())
	if err != nil {
		return fmt.Errorf("install or upgrade calico: %w", err)
	}

	return nil
}

// getCalicoValues returns the Helm values for Calico based on the distribution.
func (c *CalicoInstaller) getCalicoValues() map[string]string {
	values := defaultCalicoValues()

	// Add distribution-specific values
	switch c.distribution {
	case DistributionTalosInDocker:
		// Talos-specific settings from https://docs.siderolabs.com/kubernetes-guides/cni/deploy-calico
		maps.Copy(values, talosCalicoValues())
	case DistributionKind, DistributionK3d:
		// Kind and K3d use default values
	}

	return values
}

func defaultCalicoValues() map[string]string {
	return map[string]string{}
}

// talosCalicoValues returns Talos-specific Calico configuration.
// These settings are required for Calico to work correctly on Talos Linux.
// See: https://docs.siderolabs.com/kubernetes-guides/cni/deploy-calico
// See: https://github.com/projectcalico/calico/blob/main/charts/tigera-operator/values.yaml
func talosCalicoValues() map[string]string {
	return map[string]string{
		// Talos uses a read-only filesystem, so kubelet volume plugin path must be None
		// This is under installation. in the Helm chart values.yaml
		"installation.kubeletVolumePluginPath": `"None"`,
		// Use NFTables dataplane which is recommended for Talos
		"installation.calicoNetwork.linuxDataplane": `"Nftables"`,
		// Disable BGP for Docker-based environments
		"installation.calicoNetwork.bgp": `"Disabled"`,
		// Use VXLAN encapsulation for overlay networking
		"installation.calicoNetwork.ipPools[0].encapsulation": `"VXLAN"`,
		"installation.calicoNetwork.ipPools[0].natOutgoing":   `"Enabled"`,
		"installation.calicoNetwork.ipPools[0].nodeSelector":  `"all()"`,
		"installation.calicoNetwork.ipPools[0].blockSize":     "26",
		"installation.calicoNetwork.ipPools[0].cidr":          `"10.244.0.0/16"`,
	}
}

func (c *CalicoInstaller) waitForReadiness(ctx context.Context) error {
	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: "tigera-operator", Name: "tigera-operator"},
		{Type: "daemonset", Namespace: "calico-system", Name: "calico-node"},
		{Type: "deployment", Namespace: "calico-system", Name: "calico-kube-controllers"},
	}

	err := installer.WaitForResourceReadiness(
		ctx,
		c.GetKubeconfig(),
		c.GetContext(),
		checks,
		c.GetTimeout(),
		"calico",
	)
	if err != nil {
		return fmt.Errorf("wait for calico readiness: %w", err)
	}

	return nil
}
