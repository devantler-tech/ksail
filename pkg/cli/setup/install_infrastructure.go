package setup

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cloudproviderkind"
	hcloudccminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hcloudccm"
	metallbinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metallb"
	metricsserverinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metricsserver"
	"k8s.io/client-go/tools/clientcmd"
)

// HelmClientForCluster creates a Helm client configured for the cluster.
// It validates that the kubeconfig file exists and that the specified context
// is present in the kubeconfig before creating the Helm client.
func HelmClientForCluster(clusterCfg *v1alpha1.Cluster) (*helm.Client, string, error) {
	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get kubeconfig path: %w", err)
	}

	// Validate file exists
	_, err = os.Stat(kubeconfigPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to access kubeconfig file: %w", err)
	}

	kubeContext := clusterCfg.Spec.Cluster.Connection.Context

	// Validate the context exists in the kubeconfig to prevent nil pointer
	// dereference panics in Helm v4, which defers context validation until
	// the REST client is actually used (e.g., in IsReachable).
	if kubeContext != "" {
		err := validateKubeconfigContext(kubeconfigPath, kubeContext)
		if err != nil {
			return nil, "", err
		}
	}

	helmClient, err := helm.NewClient(kubeconfigPath, kubeContext)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Helm client: %w", err)
	}

	return helmClient, kubeconfigPath, nil
}

// validateKubeconfigContext checks that the specified context exists in the kubeconfig file.
// Returns a descriptive error listing available contexts when the target context is missing.
func validateKubeconfigContext(kubeconfigPath, contextName string) error {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig for context validation: %w", err)
	}

	if _, exists := config.Contexts[contextName]; exists {
		return nil
	}

	available := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		available = append(available, name)
	}

	sort.Strings(available)

	return fmt.Errorf(
		"%w: %q not found in %s (available: %s)",
		k8s.ErrKubeconfigContextNotFound,
		contextName, kubeconfigPath, strings.Join(available, ", "),
	)
}

// NeedsMetricsServerInstall determines if metrics-server needs to be installed.
// Returns true only when MetricsServer is Enabled AND the distribution doesn't provide it by default.
// When MetricsServer is Default, we don't install (rely on distribution's default behavior).
func NeedsMetricsServerInstall(clusterCfg *v1alpha1.Cluster) bool {
	if clusterCfg.Spec.Cluster.MetricsServer != v1alpha1.MetricsServerEnabled {
		return false
	}

	// Don't install if distribution provides it by default
	return !clusterCfg.Spec.Cluster.Distribution.ProvidesMetricsServerByDefault()
}

// NeedsLoadBalancerInstall determines if LoadBalancer support needs to be installed.
//
// In general, we install LoadBalancer only when it is explicitly Enabled AND the
// distribution × provider combination does not provide it by default.
//
// Special case:
//   - Talos × Hetzner: hcloud-ccm is not pre-installed and must be installed
//     by KSail when LoadBalancer is either Default or Enabled.
func NeedsLoadBalancerInstall(clusterCfg *v1alpha1.Cluster) bool {
	dist := clusterCfg.Spec.Cluster.Distribution
	provider := clusterCfg.Spec.Cluster.Provider
	lbSetting := clusterCfg.Spec.Cluster.LoadBalancer

	// KWOK is a simulation cluster with no real network dataplane. LoadBalancer
	// installation is always a no-op on KWOK regardless of the setting, so skip it
	// entirely to keep component counts and update diffs consistent.
	if dist == v1alpha1.DistributionKWOK {
		return false
	}

	// Special handling for Talos clusters on Hetzner:
	// According to the distribution × provider matrix, hcloud-ccm must be
	// installed by KSail for both Default and Enabled LoadBalancer settings.
	if dist == v1alpha1.DistributionTalos && provider == v1alpha1.ProviderHetzner {
		return lbSetting == v1alpha1.LoadBalancerDefault ||
			lbSetting == v1alpha1.LoadBalancerEnabled
	}

	// Generic behavior for all other distribution × provider combinations.
	if lbSetting != v1alpha1.LoadBalancerEnabled {
		return false
	}

	// Don't install if distribution × provider provides it by default.
	return !dist.ProvidesLoadBalancerByDefault(provider)
}

// helmClientSetup creates a Helm client and retrieves the install timeout.
func helmClientSetup(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) (*helm.Client, string, time.Duration, error) {
	helmClient, kubeconfigPath, err := factories.HelmClientFactory(clusterCfg)
	if err != nil {
		return nil, "", 0, fmt.Errorf("failed to create helm client: %w", err)
	}

	timeout := installer.GetInstallTimeout(clusterCfg)

	return helmClient, kubeconfigPath, timeout, nil
}

// InstallMetricsServerSilent installs metrics-server silently for parallel execution.
func InstallMetricsServerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	helmClient, _, timeout, err := helmClientSetup(clusterCfg, factories)
	if err != nil {
		return err
	}

	msInstaller := metricsserverinstaller.NewInstallerWithDistribution(
		helmClient,
		timeout,
		clusterCfg.Spec.Cluster.Distribution,
	)

	installErr := msInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("metrics-server installation failed: %w", installErr)
	}

	return nil
}

// InstallLoadBalancerSilent installs LoadBalancer support silently for parallel execution.
// For Vanilla (Kind) × Docker, starts the Cloud Provider KIND controller as a Docker container.
// For Talos × Docker, installs MetalLB.
// For Talos × Hetzner, installs hcloud-cloud-controller-manager.
func InstallLoadBalancerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return installCloudProviderKind(ctx, clusterCfg)
	case v1alpha1.DistributionTalos:
		switch clusterCfg.Spec.Cluster.Provider {
		case v1alpha1.ProviderDocker:
			return installMetalLB(ctx, clusterCfg, factories)
		case v1alpha1.ProviderHetzner:
			return installHcloudCCM(ctx, clusterCfg, factories)
		case v1alpha1.ProviderOmni:
			// Omni manages the machine lifecycle; MetalLB is not applicable
			return nil
		case v1alpha1.ProviderAWS:
			// AWS is not a supported provider for Talos.
			return nil
		}
	case v1alpha1.DistributionK3s:
		// K3s already has ServiceLB (Klipper) by default, no installation needed
		return nil
	case v1alpha1.DistributionVCluster, v1alpha1.DistributionKWOK, v1alpha1.DistributionEKS:
		// VCluster (Vind) handles LoadBalancer via its own networking.
		// KWOK is a simulation cluster with no real network dataplane.
		// EKS relies on AWS Load Balancer Controller (installed separately).
		return nil
	}

	return nil
}

// installCloudProviderKind installs the Cloud Provider KIND controller for Vanilla × Docker.
func installCloudProviderKind(ctx context.Context, clusterCfg *v1alpha1.Cluster) error {
	if clusterCfg.Spec.Cluster.Provider != v1alpha1.ProviderDocker {
		return nil
	}

	dockerAPIClient, dockErr := dockerclient.GetDockerClient()
	if dockErr != nil {
		return fmt.Errorf("create docker client: %w", dockErr)
	}

	defer func() { _ = dockerAPIClient.Close() }()

	lbInstaller := cloudproviderkindinstaller.NewInstaller(dockerAPIClient)

	installErr := lbInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("cloud-provider-kind installation failed: %w", installErr)
	}

	return nil
}

// installMetalLB installs MetalLB for Talos × Docker LoadBalancer support.
func installMetalLB(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	if clusterCfg.Spec.Cluster.Provider != v1alpha1.ProviderDocker {
		return nil
	}

	helmClient, kubeconfigPath, timeout, err := helmClientSetup(clusterCfg, factories)
	if err != nil {
		return fmt.Errorf("failed to setup helm client for metallb: %w", err)
	}

	lbInstaller := metallbinstaller.NewInstaller(
		helmClient,
		kubeconfigPath,
		clusterCfg.Spec.Cluster.Connection.Context,
		timeout,
		"", // Use default IP range
	)

	installErr := lbInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("metallb installation failed: %w", installErr)
	}

	return nil
}

// installHcloudCCM installs the Hetzner Cloud Controller Manager for Talos × Hetzner LoadBalancer support.
func installHcloudCCM(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	if clusterCfg.Spec.Cluster.Provider != v1alpha1.ProviderHetzner {
		return nil
	}

	helmClient, kubeconfigPath, timeout, err := helmClientSetup(clusterCfg, factories)
	if err != nil {
		return fmt.Errorf("failed to setup helm client for hcloud-ccm: %w", err)
	}

	ccmInstaller := hcloudccminstaller.NewInstaller(
		helmClient,
		kubeconfigPath,
		clusterCfg.Spec.Cluster.Connection.Context,
		timeout,
	)

	installErr := ccmInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("hcloud-ccm installation failed: %w", installErr)
	}

	return nil
}

// installKubeletCSRApproverSilent installs kubelet-csr-approver silently for parallel execution.
// kubelet-csr-approver is required when metrics-server is installed with secure TLS enabled,
// as it automatically approves kubelet serving certificate CSRs.
func installKubeletCSRApproverSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	return installFromFactory(
		ctx, clusterCfg, factories.KubeletCSRApprover,
		ErrKubeletCSRApproverInstallerFactoryNil, "kubelet-csr-approver",
	)
}

// InstallCSISilent installs CSI silently for parallel execution.
func InstallCSISilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	return installFromFactory(
		ctx, clusterCfg, factories.CSI,
		ErrCSIInstallerFactoryNil, "CSI",
	)
}

// InstallCertManagerSilent installs cert-manager silently for parallel execution.
func InstallCertManagerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	return installFromFactory(
		ctx, clusterCfg, factories.CertManager,
		ErrCertManagerInstallerFactoryNil, "cert-manager",
	)
}

// InstallPolicyEngineSilent installs the policy engine silently for parallel execution.
func InstallPolicyEngineSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	return installFromFactory(
		ctx, clusterCfg, factories.PolicyEngine,
		ErrPolicyEngineInstallerFactoryNil, "policy-engine",
	)
}

// installFromFactory is a shared helper for Install*Silent functions that follow the
// factory pattern: check factory != nil, create installer, call Install.
func installFromFactory(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
	nilErr error,
	componentName string,
) error {
	if factory == nil {
		return nilErr
	}

	inst, err := factory(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create %s installer: %w", componentName, err)
	}

	installErr := inst.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("%s installation failed: %w", componentName, installErr)
	}

	return nil
}
