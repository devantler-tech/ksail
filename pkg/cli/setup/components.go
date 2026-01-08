package setup

import (
	"context"
	"errors"
	"fmt"
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
	fluxinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/flux"
	gatekeeperinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/gatekeeper"
	kyvernoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/kyverno"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/localpathstorage"
	metricsserverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/metrics-server"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
)

// Errors for component installation.
var (
	ErrCertManagerInstallerFactoryNil = errors.New("cert-manager installer factory is nil")
	ErrArgoCDInstallerFactoryNil      = errors.New("argocd installer factory is nil")
	ErrClusterConfigNil               = errors.New("cluster config is nil")
)

// InstallerFactories holds factory functions for creating component installers.
// These can be overridden in tests for dependency injection.
type InstallerFactories struct {
	Flux         func(client helm.Interface, timeout time.Duration) installer.Installer
	CertManager  func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	CSI          func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	PolicyEngine func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	ArgoCD       func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	// EnsureArgoCDResources configures default Argo CD resources post-install.
	EnsureArgoCDResources func(
		ctx context.Context, kubeconfig string, clusterCfg *v1alpha1.Cluster, clusterName string,
	) error
	// EnsureFluxResources enforces default Flux resources post-install.
	EnsureFluxResources func(
		ctx context.Context, kubeconfig string, clusterCfg *v1alpha1.Cluster, clusterName string,
	) error
	// HelmClientFactory creates a Helm client for the cluster.
	HelmClientFactory func(clusterCfg *v1alpha1.Cluster) (*helm.Client, string, error)
}

// DefaultInstallerFactories returns the default installer factories.
//
//nolint:funlen // Factory function with multiple closures requires this length.
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

		return certmanagerinstaller.NewCertManagerInstaller(helmClient, timeout), nil
	}

	factories.CSI = func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		_, kubeconfig, err := factories.HelmClientFactory(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)

		return localpathstorageinstaller.NewLocalPathStorageInstaller(
			kubeconfig,
			clusterCfg.Spec.Cluster.Connection.Context,
			timeout,
			clusterCfg.Spec.Cluster.Distribution,
		), nil
	}

	factories.ArgoCD = func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, _, err := factories.HelmClientFactory(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)

		return argocdinstaller.NewArgoCDInstaller(helmClient, timeout), nil
	}

	factories.PolicyEngine = func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
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
			return kyvernoinstaller.NewKyvernoInstaller(helmClient, timeout), nil
		case v1alpha1.PolicyEngineGatekeeper:
			return gatekeeperinstaller.NewGatekeeperInstaller(helmClient, timeout), nil
		default:
			return nil, fmt.Errorf("%w: unknown engine %q", ErrPolicyEngineDisabled, engine)
		}
	}

	factories.EnsureArgoCDResources = EnsureArgoCDResources
	factories.EnsureFluxResources = fluxinstaller.EnsureDefaultResources

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

// InstallMetricsServerSilent installs metrics-server silently for parallel execution.
func InstallMetricsServerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	helmClient, kubeconfig, err := factories.HelmClientFactory(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create helm client: %w", err)
	}

	timeout := installer.GetInstallTimeout(clusterCfg)
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

// ErrCSIInstallerFactoryNil is returned when the CSI installer factory is nil.
var ErrCSIInstallerFactoryNil = errors.New("CSI installer factory is nil")

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

// ErrPolicyEngineInstallerFactoryNil is returned when the policy engine installer factory is nil.
var ErrPolicyEngineInstallerFactoryNil = errors.New("policy engine installer factory is nil")

// ErrPolicyEngineDisabled is returned when the policy engine is disabled.
var ErrPolicyEngineDisabled = errors.New("policy engine is disabled")

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

	// Skip installation if no policy engine is configured
	if peInstaller == nil {
		return nil
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

	sourceDir := strings.TrimSpace(clusterCfg.Spec.Workload.SourceDirectory)
	if sourceDir == "" {
		sourceDir = v1alpha1.DefaultSourceDirectory
	}

	repoName := registry.SanitizeRepoName(sourceDir)
	// Use cluster-prefixed local registry name for in-cluster DNS resolution
	registryHost := registry.BuildLocalRegistryName(clusterName)
	hostPort := net.JoinHostPort(
		registryHost,
		strconv.Itoa(dockerclient.DefaultRegistryPort),
	)
	repoURL := fmt.Sprintf("oci://%s/%s", hostPort, repoName)

	// SourcePath is "." because the OCI artifact contains the source directory contents
	// at the root level, not in a subdirectory.
	err = mgr.Ensure(ctx, argocdgitops.EnsureOptions{
		RepositoryURL:   repoURL,
		SourcePath:      ".",
		ApplicationName: "ksail",
		TargetRevision:  "dev",
	})
	if err != nil {
		return fmt.Errorf("ensure argocd resources: %w", err)
	}

	return nil
}
