package ciliuminstaller

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

// CiliumInstaller implements the installer.Installer interface for Cilium.
type CiliumInstaller struct {
	*cni.InstallerBase

	distribution Distribution
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
	distribution Distribution,
) *CiliumInstaller {
	ciliumInstaller := &CiliumInstaller{
		distribution: distribution,
	}
	ciliumInstaller.InstallerBase = cni.NewInstallerBase(
		client,
		kubeconfig,
		context,
		timeout,
		ciliumInstaller.waitForReadiness,
	)

	return ciliumInstaller
}

// Install installs or upgrades Cilium via its Helm chart.
func (c *CiliumInstaller) Install(ctx context.Context) error {
	err := c.helmInstallOrUpgradeCilium(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Cilium: %w", err)
	}

	return nil
}

// SetWaitForReadinessFunc overrides the readiness wait function. Primarily used for testing.
func (c *CiliumInstaller) SetWaitForReadinessFunc(waitFunc func(context.Context) error) {
	c.InstallerBase.SetWaitForReadinessFunc(waitFunc, c.waitForReadiness)
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
	case DistributionTalosInDocker:
		// Talos-specific settings from https://docs.siderolabs.com/kubernetes-guides/cni/deploying-cilium
		maps.Copy(values, talosCiliumValues())
	case DistributionKind, DistributionK3d:
		// Kind and K3d use default values
	}

	return values
}

func defaultCiliumValues() map[string]string {
	return map[string]string{
		"operator.replicas": "1",
	}
}

// talosCiliumValues returns Talos-specific Cilium configuration.
// These settings are required for Cilium to work correctly on Talos Linux.
// See: https://docs.siderolabs.com/kubernetes-guides/cni/deploying-cilium
func talosCiliumValues() map[string]string {
	// Talos does not allow loading kernel modules by Kubernetes workloads,
	// so SYS_MODULE capability must be dropped from ciliumAgent.
	// The capability list below excludes SYS_MODULE from the default set.
	ciliumAgentCaps := "{CHOWN,KILL,NET_ADMIN,NET_RAW,IPC_LOCK," +
		"SYS_ADMIN,SYS_RESOURCE,DAC_OVERRIDE,FOWNER,SETGID,SETUID}"
	cleanCiliumStateCaps := "{NET_ADMIN,SYS_ADMIN,SYS_RESOURCE}"

	return map[string]string{
		// IPAM mode set to kubernetes as recommended for Talos
		"ipam.mode": "kubernetes",
		// Talos mounts cgroupv2 at /sys/fs/cgroup, disable auto-mount
		"cgroup.autoMount.enabled":                      "false",
		"cgroup.hostRoot":                               "/sys/fs/cgroup",
		"securityContext.capabilities.ciliumAgent":      ciliumAgentCaps,
		"securityContext.capabilities.cleanCiliumState": cleanCiliumStateCaps,
	}
}

func (c *CiliumInstaller) waitForReadiness(ctx context.Context) error {
	checks := []k8s.ReadinessCheck{
		{Type: "daemonset", Namespace: "kube-system", Name: "cilium"},
		{Type: "deployment", Namespace: "kube-system", Name: "cilium-operator"},
	}

	err := installer.WaitForResourceReadiness(
		ctx,
		c.GetKubeconfig(),
		c.GetContext(),
		checks,
		c.GetTimeout(),
		"cilium",
	)
	if err != nil {
		return fmt.Errorf("wait for cilium readiness: %w", err)
	}

	return nil
}
