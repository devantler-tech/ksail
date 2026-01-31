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
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	argocdgitops "github.com/devantler-tech/ksail/v5/pkg/client/argocd"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	argocdinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/argocd"
	certmanagerinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cert-manager"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cloudproviderkind"
	fluxinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/flux"
	gatekeeperinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/gatekeeper"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/hetzner-csi"
	kubeletcsrapproverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/kubelet-csr-approver"
	kyvernoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/kyverno"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/localpathstorage"
	metricsserverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/metrics-server"
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
			timeout = installer.MaxTimeout(timeout, installer.KyvernoInstallTimeout)

			return kyvernoinstaller.NewKyvernoInstaller(helmClient, timeout), nil
		case v1alpha1.PolicyEngineGatekeeper:
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
) func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	return func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, _, err := factories.HelmClientFactory(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)

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

	factories.CertManager = func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, _, err := factories.HelmClientFactory(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)
		timeout = installer.MaxTimeout(timeout, installer.CertManagerInstallTimeout)

		return certmanagerinstaller.NewCertManagerInstaller(helmClient, timeout), nil
	}
	factories.ArgoCD = helmInstallerFactory(
		factories,
		func(c helm.Interface, t time.Duration) installer.Installer {
			return argocdinstaller.NewArgoCDInstaller(c, t)
		},
	)
	factories.KubeletCSRApprover = helmInstallerFactory(
		factories,
		func(c helm.Interface, t time.Duration) installer.Installer {
			return kubeletcsrapproverinstaller.NewKubeletCSRApproverInstaller(c, t)
		},
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
	kubeconfig, err := helpers.GetKubeconfigPathFromConfig(clusterCfg)
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
// For Vanilla (Kind) × Docker, installs Cloud Provider KIND.
func InstallLoadBalancerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	helmClient, kubeconfig, timeout, err := helmClientSetup(clusterCfg, factories)
	if err != nil {
		return err
	}

	// Determine which LoadBalancer implementation to install based on distribution × provider
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		// Vanilla (Kind) × Docker uses Cloud Provider KIND
		if clusterCfg.Spec.Cluster.Provider == v1alpha1.ProviderDocker {
			lbInstaller := cloudproviderkindinstaller.NewCloudProviderKINDInstaller(
				helmClient,
				kubeconfig,
				clusterCfg.Spec.Cluster.Connection.Context,
				timeout,
			)

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

		// Talos × Docker: MetalLB is planned but not yet implemented in ksail,
		// so installing a LoadBalancer implementation is currently unsupported.
		return fmt.Errorf("%w for Talos with provider %s", v1alpha1.ErrLoadBalancerNotImplemented, clusterCfg.Spec.Cluster.Provider)
	case v1alpha1.DistributionK3s:
		// K3s already has ServiceLB (Klipper) by default, no installation needed
		return nil
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
	if factories.KubeletCSRApprover == nil {
		return ErrKubeletCSRApproverInstallerFactoryNil
	}

	csrApproverInstaller, err := factories.KubeletCSRApprover(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create kubelet-csr-approver installer: %w", err)
	}

	installErr := csrApproverInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("kubelet-csr-approver installation failed: %w", installErr)
	}

	return nil
}

// InstallArgoCDSilent installs ArgoCD silently for parallel execution.
func InstallArgoCDSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	if factories.ArgoCD == nil {
		return ErrArgoCDInstallerFactoryNil
	}

	argoInstaller, err := factories.ArgoCD(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create argocd installer: %w", err)
	}

	installErr := argoInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("failed to install argocd: %w", installErr)
	}

	return nil
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
	if factories.CSI == nil {
		return ErrCSIInstallerFactoryNil
	}

	csiInstaller, err := factories.CSI(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create CSI installer: %w", err)
	}

	installErr := csiInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("local-path-storage installation failed: %w", installErr)
	}

	return nil
}

// InstallCertManagerSilent installs cert-manager silently for parallel execution.
func InstallCertManagerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	if factories.CertManager == nil {
		return ErrCertManagerInstallerFactoryNil
	}

	cmInstaller, err := factories.CertManager(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create cert-manager installer: %w", err)
	}

	installErr := cmInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("failed to install cert-manager: %w", installErr)
	}

	return nil
}

// InstallPolicyEngineSilent installs the policy engine silently for parallel execution.
func InstallPolicyEngineSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	if factories.PolicyEngine == nil {
		return ErrPolicyEngineInstallerFactoryNil
	}

	peInstaller, err := factories.PolicyEngine(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create policy engine installer: %w", err)
	}

	installErr := peInstaller.Install(ctx)
	if installErr != nil {
		return fmt.Errorf("failed to install policy engine: %w", installErr)
	}

	return nil
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
