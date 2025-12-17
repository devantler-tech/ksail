package ciliuminstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail-go/pkg/client/helm"
	"github.com/devantler-tech/ksail-go/pkg/k8s"
	"github.com/devantler-tech/ksail-go/pkg/svc/installer"
	"github.com/devantler-tech/ksail-go/pkg/svc/installer/cni"
)

// CiliumInstaller implements the installer.Installer interface for Cilium.
type CiliumInstaller struct {
	*cni.InstallerBase
}

// NewCiliumInstaller creates a new Cilium installer instance.
func NewCiliumInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *CiliumInstaller {
	ciliumInstaller := &CiliumInstaller{}
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
		SetJSONVals:     defaultCiliumValues(),
	}

	err = helm.InstallOrUpgradeChart(ctx, client, repoConfig, chartConfig, c.GetTimeout())
	if err != nil {
		return fmt.Errorf("install or upgrade cilium: %w", err)
	}

	return nil
}

func defaultCiliumValues() map[string]string {
	return map[string]string{
		"operator.replicas": "1",
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
