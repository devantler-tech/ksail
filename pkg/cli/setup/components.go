package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	argocdinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/argocd"
	certmanagerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/certmanager"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	gatekeeperinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/gatekeeper"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hetznercsi"
	kubeletcsrapproverinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/kubeletcsrapprover"
	kyvernoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/kyverno"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/localpathstorage"
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
	// registryHostOverride replaces the default Docker container name in the OCI URL
	// when non-empty.
	EnsureFluxResources func(
		ctx context.Context, kubeconfig string, clusterCfg *v1alpha1.Cluster,
		clusterName string, registryHostOverride string, artifactPushed bool,
	) error
	// SetupFluxInstance creates the FluxInstance CR without waiting for readiness.
	// registryHostOverride replaces the default Docker container name in the OCI URL
	// when non-empty.
	// Use with WaitForFluxReady after pushing artifacts.
	SetupFluxInstance func(
		ctx context.Context, kubeconfig string, clusterCfg *v1alpha1.Cluster, clusterName string, registryHostOverride string,
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

			return kyvernoinstaller.NewInstaller(helmClient, timeout), nil
		case v1alpha1.PolicyEngineGatekeeper:
			timeout = max(timeout, installer.GatekeeperInstallTimeout)

			return gatekeeperinstaller.NewInstaller(helmClient, timeout), nil
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
			return hetznercsiinstaller.NewInstaller(
				helmClient,
				kubeconfig,
				clusterCfg.Spec.Cluster.Connection.Context,
				timeout,
			), nil
		}

		// For other distributions, use local-path-provisioner
		return localpathstorageinstaller.NewInstaller(
			kubeconfig,
			clusterCfg.Spec.Cluster.Connection.Context,
			timeout,
			clusterCfg.Spec.Cluster.Distribution,
		), nil
	}
}

// resolveHelmClientAndTimeout creates a Helm client and computes the
// effective install timeout for the given cluster configuration.
func resolveHelmClientAndTimeout(
	factories *InstallerFactories,
	clusterCfg *v1alpha1.Cluster,
	minTimeout time.Duration,
) (helm.Interface, time.Duration, error) {
	helmClient, _, err := factories.HelmClientFactory(clusterCfg)
	if err != nil {
		return nil, 0, err
	}

	timeout := max(
		installer.GetInstallTimeout(clusterCfg), minTimeout,
	)

	return helmClient, timeout, nil
}

// helmInstallerFactory creates a factory function for helm-based installers.
func helmInstallerFactory(
	factories *InstallerFactories,
	newInstaller func(client helm.Interface, timeout time.Duration) installer.Installer,
	minTimeout time.Duration,
) func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	return func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, timeout, err := resolveHelmClientAndTimeout(
			factories, clusterCfg, minTimeout,
		)
		if err != nil {
			return nil, err
		}

		return newInstaller(helmClient, timeout), nil
	}
}

// argoCDInstallerFactory creates a factory for the ArgoCD installer
// that evaluates SOPS configuration from the cluster spec.
func argoCDInstallerFactory(
	factories *InstallerFactories,
) func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	return func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, timeout, err := resolveHelmClientAndTimeout(
			factories, clusterCfg,
			installer.ArgoCDInstallTimeout,
		)
		if err != nil {
			return nil, err
		}

		sopsEnabled := argocdinstaller.ShouldEnableSOPS(
			clusterCfg.Spec.Cluster.SOPS,
		)

		return argocdinstaller.NewInstaller(
			helmClient, timeout, sopsEnabled,
		), nil
	}
}

// DefaultInstallerFactories returns the default installer factories.
func DefaultInstallerFactories() *InstallerFactories {
	factories := &InstallerFactories{}

	// Set HelmClientFactory first as other factories depend on it
	factories.HelmClientFactory = HelmClientForCluster

	factories.Flux = func(client helm.Interface, timeout time.Duration) installer.Installer {
		return fluxinstaller.NewInstaller(client, timeout)
	}

	factories.CertManager = helmInstallerFactory(
		factories,
		func(c helm.Interface, t time.Duration) installer.Installer {
			return certmanagerinstaller.NewInstaller(c, t)
		},
		installer.CertManagerInstallTimeout,
	)
	factories.ArgoCD = argoCDInstallerFactory(factories)
	factories.KubeletCSRApprover = helmInstallerFactory(
		factories,
		func(c helm.Interface, t time.Duration) installer.Installer {
			return kubeletcsrapproverinstaller.NewInstaller(c, t)
		},
		0,
	)
	factories.CSI = csiFactory(factories)
	factories.PolicyEngine = policyEngineFactory(factories)

	factories.EnsureArgoCDResources = EnsureArgoCDResources
	factories.EnsureFluxResources = fluxinstaller.EnsureDefaultResources
	factories.SetupFluxInstance = fluxinstaller.SetupInstance
	factories.WaitForFluxReady = fluxinstaller.WaitForFluxReady

	return factories
}
