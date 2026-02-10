package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers/kubeconfig"
	argocdgitops "github.com/devantler-tech/ksail/v5/pkg/client/argocd"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	argocdinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/argocd"
	certmanagerinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/certmanager"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cloudproviderkind"
	fluxinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/flux"
	gatekeeperinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/gatekeeper"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/hetznercsi"
	kubeletcsrapproverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/kubeletcsrapprover"
	kyvernoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/kyverno"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/localpathstorage"
	metricsserverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/metricsserver"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/spf13/cobra"
)

// Errors for component installation.
var (
	ErrCertManagerInstallerFactoryNil        = errors.New("cert-manager installer factory is nil")
	ErrArgoCDInstallerFactoryNil             = errors.New("argocd installer factory is nil")
	ErrKubeletCSRApproverInstallerFactoryNil = errors.New(
		"kubelet-csr-approver installer factory is nil",
	)
	ErrCSIInstallerFactoryNil          = errors.New("CSI installer factory is nil")
	ErrPolicyEngineInstallerFactoryNil = errors.New("policy engine installer factory is nil")
	ErrPolicyEngineDisabled            = errors.New("policy engine is disabled")
	ErrClusterConfigNil                = errors.New("cluster config is nil")
)

// InstallerFactories holds factory functions for creating component installers.
// These can be overridden in tests for dependency injection.
type InstallerFactories struct {
	Flux               func(client helm.Interface, timeout time.Duration) installer.Installer
	CertManager        func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	CSI                func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	PolicyEngine       func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	ArgoCD             func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	KubeletCSRApprover func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	// EnsureArgoCDResources configures default Argo CD resources post-install.
	EnsureArgoCDResources func(
		ctx context.Context, kubeconfig string, clusterCfg *v1alpha1.Cluster, clusterName string,
	) error
	// EnsureFluxResources enforces default Flux resources post-install.
	// If artifactPushed is false, the function will skip waiting for FluxInstance readiness
	// because the artifact doesn't exist yet (will be pushed later via workload push).
	EnsureFluxResources func(
		ctx context.Context, kubeconfig string, clusterCfg *v1alpha1.Cluster, clusterName string, artifactPushed bool,
	) error
	// SetupFluxInstance creates the FluxInstance CR without waiting for readiness.
	// Use with WaitForFluxReady after pushing artifacts.
	SetupFluxInstance func(
		ctx context.Context, kubeconfig string, clusterCfg *v1alpha1.Cluster, clusterName string,
	) error
	// WaitForFluxReady waits for the FluxInstance to be ready.
	// Call after pushing OCI artifacts.
	WaitForFluxReady func(ctx context.Context, kubeconfig string) error
	// EnsureOCIArtifact checks if an OCI artifact exists and pushes one if needed.
	// Returns true if artifact exists or was pushed, false if not needed.
	// Set to nil to use the default implementation.
	EnsureOCIArtifact func(
		ctx context.Context, cmd *cobra.Command, clusterCfg *v1alpha1.Cluster, clusterName string, writer io.Writer,
	) (bool, error)
	// HelmClientFactory creates a Helm client for the cluster.
	HelmClientFactory func(clusterCfg *v1alpha1.Cluster) (*helm.Client, string, error)
}

// policyEngineFactory creates the policy engine factory function.
func policyEngineFactory(
	factories *InstallerFactories,
) func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	return func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		engine := clusterCfg.Spec.Cluster.PolicyEngine

		// Early return for disabled policy engine
		if engine == v1alpha1.PolicyEngineNone || engine == "" {
			return nil, ErrPolicyEngineDisabled
		}

		helmClient, _, err := factories.HelmClientFactory(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)

		//nolint:exhaustive // PolicyEngineNone is handled above with early return
		switch engine {
		case v1alpha1.PolicyEngineKyverno:
			timeout = max(timeout, installer.KyvernoInstallTimeout)

			return kyvernoinstaller.NewKyvernoInstaller(helmClient, timeout), nil
		case v1alpha1.PolicyEngineGatekeeper:
			timeout = max(timeout, installer.GatekeeperInstallTimeout)

			return gatekeeperinstaller.NewGatekeeperInstaller(helmClient, timeout), nil
		default:
			return nil, fmt.Errorf("%w: unknown engine %q", ErrPolicyEngineDisabled, engine)
		}
	}
}

// csiFactory creates the CSI factory function.
func csiFactory(
	factories *InstallerFactories,
) func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	return func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, kubeconfig, err := factories.HelmClientFactory(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)

		// For Talos × Hetzner, use the Hetzner CSI driver
		if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos &&
			clusterCfg.Spec.Cluster.Provider == v1alpha1.ProviderHetzner {
			return hetznercsiinstaller.NewHetznerCSIInstaller(
				helmClient,
				kubeconfig,
				clusterCfg.Spec.Cluster.Connection.Context,
				timeout,
			), nil
		}

		// For other distributions, use local-path-provisioner
		return localpathstorageinstaller.NewLocalPathStorageInstaller(
			kubeconfig,
			clusterCfg.Spec.Cluster.Connection.Context,
			timeout,
			clusterCfg.Spec.Cluster.Distribution,
		), nil
	}
}

// helmInstallerFactory creates a factory function for helm-based installers.
func helmInstallerFactory(
	factories *InstallerFactories,
	newInstaller func(client helm.Interface, timeout time.Duration) installer.Installer,
	minTimeout time.Duration,
) func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	return func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, _, err := factories.HelmClientFactory(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)
		timeout = max(timeout, minTimeout)

		return newInstaller(helmClient, timeout), nil
	}
}

// DefaultInstallerFactories returns the default installer factories.
func DefaultInstallerFactories() *InstallerFactories {
	factories := &InstallerFactories{}

	// Set HelmClientFactory first as other factories depend on it
	factories.HelmClientFactory = HelmClientForCluster

	factories.Flux = func(client helm.Interface, timeout time.Duration) installer.Installer {
		return fluxinstaller.NewFluxInstaller(client, timeout)
	}

	factories.CertManager = helmInstallerFactory(
		factories,
		func(c helm.Interface, t time.Duration) installer.Installer {
			return certmanagerinstaller.NewCertManagerInstaller(c, t)
		},
		installer.CertManagerInstallTimeout,
	)
	factories.ArgoCD = helmInstallerFactory(
		factories,
		func(c helm.Interface, t time.Duration) installer.Installer {
			return argocdinstaller.NewArgoCDInstaller(c, t)
		},
		installer.ArgoCDInstallTimeout,
	)
	factories.KubeletCSRApprover = helmInstallerFactory(
		factories,
		func(c helm.Interface, t time.Duration) installer.Installer {
			return kubeletcsrapproverinstaller.NewKubeletCSRApproverInstaller(c, t)
		},
		0,
	)
	factories.CSI = csiFactory(factories)
	factories.PolicyEngine = policyEngineFactory(factories)

	factories.EnsureArgoCDResources = EnsureArgoCDResources
	factories.EnsureFluxResources = fluxinstaller.EnsureDefaultResources
	factories.SetupFluxInstance = fluxinstaller.SetupFluxInstance
	factories.WaitForFluxReady = fluxinstaller.WaitForFluxReady

	return factories
}

// HelmClientForCluster creates a Helm client configured for the cluster.
func HelmClientForCluster(clusterCfg *v1alpha1.Cluster) (*helm.Client, string, error) {
	kubeconfig, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get kubeconfig path: %w", err)
	}

	// Validate file exists
	_, err = os.Stat(kubeconfig)
	if err != nil {
		return nil, "", fmt.Errorf("failed to access kubeconfig file: %w", err)
	}

	helmClient, err := helm.NewClient(kubeconfig, clusterCfg.Spec.Cluster.Connection.Context)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Helm client: %w", err)
	}

	return helmClient, kubeconfig, nil
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
// Returns true only when LoadBalancer is Enabled AND the distribution × provider doesn't provide it by default.
// When LoadBalancer is Default, we don't install (rely on distribution × provider default behavior).
func NeedsLoadBalancerInstall(clusterCfg *v1alpha1.Cluster) bool {
	if clusterCfg.Spec.Cluster.LoadBalancer != v1alpha1.LoadBalancerEnabled {
		return false
	}

	// Don't install if distribution × provider provides it by default
	return !clusterCfg.Spec.Cluster.Distribution.ProvidesLoadBalancerByDefault(
		clusterCfg.Spec.Cluster.Provider,
	)
}

// helmClientSetup creates a Helm client and retrieves the install timeout.
// Returns the Helm client, kubeconfig path, timeout, and any error.
func helmClientSetup(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) (*helm.Client, string, time.Duration, error) {
	helmClient, kubeconfig, err := factories.HelmClientFactory(clusterCfg)
	if err != nil {
		return nil, "", 0, fmt.Errorf("failed to create helm client: %w", err)
	}

	timeout := installer.GetInstallTimeout(clusterCfg)

	return helmClient, kubeconfig, timeout, nil
}

// InstallMetricsServerSilent installs metrics-server silently for parallel execution.
func InstallMetricsServerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	helmClient, kubeconfig, timeout, err := helmClientSetup(clusterCfg, factories)
	if err != nil {
		return err
	}

	msInstaller := metricsserverinstaller.NewMetricsServerInstaller(
		helmClient,
		kubeconfig,
		clusterCfg.Spec.Cluster.Connection.Context,
		timeout,
	)

	installErr := msInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("metrics-server installation failed: %w", installErr)
	}

	return nil
}

// InstallLoadBalancerSilent installs LoadBalancer support silently for parallel execution.
// For Vanilla (Kind) × Docker, starts the Cloud Provider KIND controller as a Docker container.
func InstallLoadBalancerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	_ *InstallerFactories,
) error {
	// Determine which LoadBalancer implementation to install based on distribution × provider
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		// Vanilla (Kind) × Docker uses Cloud Provider KIND
		if clusterCfg.Spec.Cluster.Provider == v1alpha1.ProviderDocker {
			// Create Docker client for container management
			dockerAPIClient, dockErr := dockerclient.GetDockerClient()
			if dockErr != nil {
				return fmt.Errorf("create docker client: %w", dockErr)
			}

			defer func() { _ = dockerAPIClient.Close() }()

			lbInstaller := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(dockerAPIClient)

			installErr := lbInstaller.Install(ctx)
			if installErr != nil {
				return fmt.Errorf("cloud-provider-kind installation failed: %w", installErr)
			}
		}
	case v1alpha1.DistributionTalos:
		// Talos × Hetzner: LoadBalancer support is expected to be provided by default
		// (via the Hetzner cloud-controller-manager / hcloud-ccm), so there is
		// nothing for this installer to do here.
		if clusterCfg.Spec.Cluster.Provider == v1alpha1.ProviderHetzner {
			return nil
		}

		// Talos × Docker: MetalLB is planned but not yet implemented in ksail.
		// For now, we skip installation (no-op) rather than failing, allowing users
		// to explicitly enable LoadBalancer in their configuration without errors.
		// MetalLB installer implementation is planned for a future release.
		return nil
	case v1alpha1.DistributionK3s:
		// K3s already has ServiceLB (Klipper) by default, no installation needed
		return nil
	}

	return nil
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

// InstallKubeletCSRApproverSilent installs kubelet-csr-approver silently for parallel execution.
// kubelet-csr-approver is required when metrics-server is installed with secure TLS enabled,
// as it automatically approves kubelet serving certificate CSRs.
func InstallKubeletCSRApproverSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	return installFromFactory(
		ctx, clusterCfg, factories.KubeletCSRApprover,
		ErrKubeletCSRApproverInstallerFactoryNil, "kubelet-csr-approver",
	)
}

// InstallArgoCDSilent installs ArgoCD silently for parallel execution.
func InstallArgoCDSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	return installFromFactory(
		ctx, clusterCfg, factories.ArgoCD,
		ErrArgoCDInstallerFactoryNil, "argocd",
	)
}

// InstallFluxSilent installs Flux silently for parallel execution.
func InstallFluxSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	helmClient, _, err := factories.HelmClientFactory(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create helm client: %w", err)
	}

	timeout := installer.GetInstallTimeout(clusterCfg)
	fluxInstaller := factories.Flux(helmClient, timeout)

	installErr := fluxInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("failed to install flux controllers: %w", installErr)
	}

	return nil
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

// EnsureArgoCDResources configures default Argo CD resources post-install.
func EnsureArgoCDResources(
	ctx context.Context,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) error {
	if clusterCfg == nil {
		return ErrClusterConfigNil
	}

	installTimeout := installer.GetInstallTimeout(clusterCfg)

	err := argocdinstaller.EnsureDefaultResources(ctx, kubeconfig, installTimeout)
	if err != nil {
		return fmt.Errorf("ensure argocd default resources: %w", err)
	}

	mgr, err := argocdgitops.NewManagerFromKubeconfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("create argocd manager: %w", err)
	}

	// Build repository URL and credentials based on registry configuration
	opts := buildArgoCDEnsureOptions(clusterCfg, clusterName)

	err = mgr.Ensure(ctx, opts)
	if err != nil {
		return fmt.Errorf("ensure argocd resources: %w", err)
	}

	return nil
}

// buildArgoCDEnsureOptions constructs ArgoCD ensure options based on registry config.
func buildArgoCDEnsureOptions(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) argocdgitops.EnsureOptions {
	opts := argocdgitops.EnsureOptions{
		SourcePath:      ".",
		ApplicationName: "ksail",
		TargetRevision:  "dev",
	}

	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry
	if localRegistry.IsExternal() {
		applyExternalRegistryOptions(&opts, localRegistry)
	} else {
		applyLocalRegistryOptions(&opts, clusterCfg, clusterName)
	}

	return opts
}

// applyExternalRegistryOptions configures options for external OCI registries.
func applyExternalRegistryOptions(
	opts *argocdgitops.EnsureOptions,
	localRegistry v1alpha1.LocalRegistry,
) {
	parsed := localRegistry.Parse()
	opts.RepositoryURL = fmt.Sprintf("oci://%s/%s", parsed.Host, parsed.Path)
	username, password := localRegistry.ResolveCredentials()
	opts.Username = username
	opts.Password = password
	opts.Insecure = false
}

// applyLocalRegistryOptions configures options for local in-cluster registries.
func applyLocalRegistryOptions(
	opts *argocdgitops.EnsureOptions,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) {
	sourceDir := strings.TrimSpace(clusterCfg.Spec.Workload.SourceDirectory)
	if sourceDir == "" {
		sourceDir = v1alpha1.DefaultSourceDirectory
	}

	repoName := registry.SanitizeRepoName(sourceDir)
	registryHost := registry.BuildLocalRegistryName(clusterName)
	hostPort := net.JoinHostPort(
		registryHost,
		strconv.Itoa(dockerclient.DefaultRegistryPort),
	)
	opts.RepositoryURL = fmt.Sprintf("oci://%s/%s", hostPort, repoName)
	opts.Insecure = true
}
