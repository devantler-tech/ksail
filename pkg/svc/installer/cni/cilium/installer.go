package ciliuminstaller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni"
)

// errGatewayAPICRDInstallerNil is returned when the Gateway API CRD installer is not configured.
var errGatewayAPICRDInstallerNil = errors.New("gateway API CRD installer is not configured")

// GatewayAPICRDInstallerFunc is a function that installs Gateway API CRDs.
type GatewayAPICRDInstallerFunc func(ctx context.Context) error

// Installer implements the installer.Installer interface for Cilium.
type Installer struct {
	*cni.InstallerBase

	distribution           v1alpha1.Distribution
	provider               v1alpha1.Provider
	loadBalancer           v1alpha1.LoadBalancer
	gatewayAPICRDInstaller GatewayAPICRDInstallerFunc
	// apiServerChecker is called for distributions that may have an API server
	// timing gap. It defaults to WaitForAPIServerStability and can be overridden
	// in tests to avoid needing a real cluster.
	apiServerChecker func(ctx context.Context) error
}

// NewInstaller creates a new Cilium installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *Installer {
	return NewInstallerWithDistribution(
		client, kubeconfig, context, timeout,
		"", "", v1alpha1.LoadBalancerDefault,
	)
}

// NewInstallerWithDistribution creates a new Cilium installer instance
// with distribution and provider-specific configuration.
func NewInstallerWithDistribution(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
	loadBalancer v1alpha1.LoadBalancer,
) *Installer {
	ciliumInstaller := &Installer{
		distribution: distribution,
		provider:     provider,
		loadBalancer: loadBalancer,
	}
	ciliumInstaller.InstallerBase = cni.NewInstallerBase(
		client,
		kubeconfig,
		context,
		timeout,
	)
	ciliumInstaller.gatewayAPICRDInstaller = ciliumInstaller.installGatewayAPICRDs
	ciliumInstaller.apiServerChecker = ciliumInstaller.WaitForAPIServerStability

	return ciliumInstaller
}

// Install installs or upgrades Cilium via its Helm chart.
func (c *Installer) Install(ctx context.Context) error {
	err := c.PrepareInstall(ctx, c.distribution, c.apiServerChecker)
	if err != nil {
		return fmt.Errorf("install: %w", err)
	}

	// Install Gateway API CRDs before Cilium, as Cilium requires them
	// to be pre-installed when gatewayAPI.enabled is true.
	if c.gatewayAPICRDInstaller == nil {
		return errGatewayAPICRDInstallerNil
	}

	err = c.gatewayAPICRDInstaller(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Gateway API CRDs: %w", err)
	}

	err = c.helmInstallOrUpgradeCilium(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Cilium: %w", err)
	}

	return nil
}

// Uninstall removes the Helm release for Cilium.
func (c *Installer) Uninstall(ctx context.Context) error {
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
func (c *Installer) Images(ctx context.Context) ([]string, error) {
	images, err := c.ImagesFromChart(ctx, c.chartSpec())
	if err != nil {
		return nil, fmt.Errorf("get cilium images: %w", err)
	}

	return images, nil
}

func (c *Installer) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName:     "cilium",
		ChartName:       "cilium/cilium",
		Namespace:       "kube-system",
		Version:         chartVersion(),
		RepoURL:         "https://helm.cilium.io",
		CreateNamespace: false,
		SetJSONVals:     c.getCiliumValues(),
		Timeout:         c.GetTimeout(),
	}
}

// --- internals ---

func (c *Installer) helmInstallOrUpgradeCilium(ctx context.Context) error {
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
		Version:         chartVersion(),
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

// getCiliumValues returns the Helm values for Cilium based on the distribution and provider.
func (c *Installer) getCiliumValues() map[string]string {
	values := defaultCiliumValues()

	// Add distribution-specific values
	switch c.distribution {
	case v1alpha1.DistributionTalos:
		// Talos-specific settings from https://docs.siderolabs.com/kubernetes-guides/cni/deploying-cilium
		maps.Copy(values, talosCiliumValues())
	case v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK,
		v1alpha1.DistributionEKS:
		// Vanilla, K3s, VCluster, KWOK, and EKS use default values.
	}

	// Add provider-specific values.
	// hostNetwork is only needed on Docker when no LoadBalancer is configured,
	// because Docker clusters use port mappings and there is no external LB.
	// When a LoadBalancer IS configured (e.g. MetalLB), it assigns IPs and
	// hostNetwork is not required.
	switch c.provider {
	case v1alpha1.ProviderDocker:
		effective := c.loadBalancer.EffectiveValue(c.distribution, c.provider)
		if effective != v1alpha1.LoadBalancerEnabled {
			maps.Copy(values, dockerCiliumValues())
		}
	case v1alpha1.ProviderHetzner, v1alpha1.ProviderOmni, v1alpha1.ProviderAWS:
		// Hetzner, Omni, and AWS use default values (Cilium is not installed
		// on EKS by KSail, but if selected it behaves like other cloud providers).
	}

	return values
}

func defaultCiliumValues() map[string]string {
	return map[string]string{
		"operator.replicas":  "1", // numeric values don't need quotes
		"gatewayAPI.enabled": "true",
	}
}

// dockerCiliumValues returns Docker-provider-specific Cilium configuration.
// hostNetwork is required because Docker clusters use port mappings from
// container to host, and there is no external load balancer to assign IPs.
// NET_BIND_SERVICE is needed for binding to privileged ports (80, 443).
func dockerCiliumValues() map[string]string {
	return map[string]string{
		"gatewayAPI.hostNetwork.enabled":                           "true",
		"envoy.securityContext.capabilities.keepCapNetBindService": "true",
		"envoy.securityContext.capabilities.envoy":                 `["NET_ADMIN","NET_BIND_SERVICE","SYS_ADMIN"]`,
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
