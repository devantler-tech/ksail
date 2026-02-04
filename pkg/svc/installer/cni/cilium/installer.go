package ciliuminstaller

import (
	"context"
	"fmt"
	"maps"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni"
)

// CiliumInstaller implements the installer.Installer interface for Cilium.
type CiliumInstaller struct {
	*cni.InstallerBase

	distribution v1alpha1.Distribution
}

// NewCiliumInstaller creates a new Cilium installer instance.
func NewCiliumInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *CiliumInstaller {
	return NewCiliumInstallerWithDistribution(client, kubeconfig, context, timeout, "")
}

// NewCiliumInstallerWithDistribution creates a new Cilium installer instance with distribution-specific configuration.
func NewCiliumInstallerWithDistribution(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
) *CiliumInstaller {
	ciliumInstaller := &CiliumInstaller{
		distribution: distribution,
	}
	ciliumInstaller.InstallerBase = cni.NewInstallerBase(
		client,
		kubeconfig,
		context,
		timeout,
	)

	return ciliumInstaller
}

// Install installs or upgrades Cilium via its Helm chart.
func (c *CiliumInstaller) Install(ctx context.Context) error {
	// For Talos, wait for API server to stabilize before CNI installation.
	// The API server may be unstable immediately after bootstrap.
	if c.distribution == v1alpha1.DistributionTalos {
		err := c.WaitForAPIServerStability(ctx)
		if err != nil {
			return fmt.Errorf("failed to wait for API server stability: %w", err)
		}
	}

	err := c.helmInstallOrUpgradeCilium(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Cilium: %w", err)
	}

	return nil
}

// Uninstall removes the Helm release for Cilium.
func (c *CiliumInstaller) Uninstall(ctx context.Context) error {
	client, err := c.GetClient()
	if err != nil {
		return fmt.Errorf("get helm client: %w", err)
	}

	err = client.UninstallRelease(ctx, "cilium", "kube-system")
	if err != nil {
		return fmt.Errorf("failed to uninstall cilium release: %w", err)
	}

	return nil
}

// Images returns the container images used by Cilium.
func (c *CiliumInstaller) Images(ctx context.Context) ([]string, error) {
	images, err := c.ImagesFromChart(ctx, c.chartSpec())
	if err != nil {
		return nil, fmt.Errorf("get cilium images: %w", err)
	}

	return images, nil
}

func (c *CiliumInstaller) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName:     "cilium",
		ChartName:       "cilium/cilium",
		Namespace:       "kube-system",
		RepoURL:         "https://helm.cilium.io",
		CreateNamespace: false,
		SetJSONVals:     c.getCiliumValues(),
		Timeout:         c.GetTimeout(),
	}
}

// --- internals ---

func (c *CiliumInstaller) helmInstallOrUpgradeCilium(ctx context.Context) error {
	client, err := c.GetClient()
	if err != nil {
		return fmt.Errorf("get helm client: %w", err)
	}

	repoConfig := helm.RepoConfig{
		Name:     "cilium",
		URL:      "https://helm.cilium.io",
		RepoName: "cilium",
	}

	chartConfig := helm.ChartConfig{
		ReleaseName:     "cilium",
		ChartName:       "cilium/cilium",
		Namespace:       "kube-system",
		RepoURL:         "https://helm.cilium.io",
		CreateNamespace: false,
		SetJSONVals:     c.getCiliumValues(),
	}

	err = helm.InstallOrUpgradeChart(ctx, client, repoConfig, chartConfig, c.GetTimeout())
	if err != nil {
		return fmt.Errorf("install or upgrade cilium: %w", err)
	}

	return nil
}

// getCiliumValues returns the Helm values for Cilium based on the distribution.
func (c *CiliumInstaller) getCiliumValues() map[string]string {
	values := defaultCiliumValues()

	// Add distribution-specific values
	switch c.distribution {
	case v1alpha1.DistributionTalos:
		// Talos-specific settings from https://docs.siderolabs.com/kubernetes-guides/cni/deploying-cilium
		maps.Copy(values, talosCiliumValues())
	case v1alpha1.DistributionVanilla, v1alpha1.DistributionK3s:
		// Vanilla and K3s use default values
	}

	return values
}

func defaultCiliumValues() map[string]string {
	return map[string]string{
		"operator.replicas": "1", // numeric values don't need quotes
	}
}

// talosCiliumValues returns Talos-specific Cilium configuration.
// These settings are required for Cilium to work correctly on Talos Linux.
// See: https://docs.siderolabs.com/kubernetes-guides/cni/deploying-cilium
func talosCiliumValues() map[string]string {
	// Talos does not allow loading kernel modules by Kubernetes workloads,
	// so SYS_MODULE capability must be dropped from ciliumAgent.
	// The capability list below excludes SYS_MODULE from the default set.
	// Values must be valid JSON since they're parsed via SetJSONVals.
	ciliumAgentCaps := `["CHOWN","KILL","NET_ADMIN","NET_RAW","IPC_LOCK",` +
		`"SYS_ADMIN","SYS_RESOURCE","DAC_OVERRIDE","FOWNER","SETGID","SETUID"]`
	cleanCiliumStateCaps := `["NET_ADMIN","SYS_ADMIN","SYS_RESOURCE"]`

	return map[string]string{
		// IPAM mode set to kubernetes as recommended for Talos
		"ipam.mode": `"kubernetes"`,
		// Enable kube-proxy replacement mode for Talos.
		// When kube-proxy is disabled in Talos (proxy.disabled: true), Cilium must
		// replace kube-proxy functionality and connect directly to the API server.
		"kubeProxyReplacement": "true",
		// Connect to API server via KubePrism (Talos's local API proxy).
		// This is required when kube-proxy is disabled because the Kubernetes service
		// IP (10.96.0.1) is not routable until Cilium is running.
		// KubePrism runs on localhost:7445 on all Talos nodes.
		// See: https://docs.siderolabs.com/kubernetes-guides/advanced-guides/kubeprism
		"k8sServiceHost": `"localhost"`,
		"k8sServicePort": `"7445"`,
		// Talos mounts cgroupv2 at /sys/fs/cgroup, disable auto-mount
		"cgroup.autoMount.enabled":                      "false",
		"cgroup.hostRoot":                               `"/sys/fs/cgroup"`,
		"securityContext.capabilities.ciliumAgent":      ciliumAgentCaps,
		"securityContext.capabilities.cleanCiliumState": cleanCiliumStateCaps,
	}
}
