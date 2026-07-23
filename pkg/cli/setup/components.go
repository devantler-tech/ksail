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
	clusterautoscalerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/clusterautoscaler"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	gatekeeperinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/gatekeeper"
	hcloudccminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hcloudccm"
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
	ErrCSIInstallerFactoryNil               = errors.New("CSI installer factory is nil")
	ErrPolicyEngineInstallerFactoryNil      = errors.New("policy engine installer factory is nil")
	ErrPolicyEngineDisabled                 = errors.New("policy engine is disabled")
	ErrClusterConfigNil                     = errors.New("cluster config is nil")
	ErrClusterAutoscalerInstallerFactoryNil = errors.New(
		"cluster-autoscaler installer factory is nil",
	)
	ErrAWSLoadBalancerControllerInstallerFactoryNil = errors.New(
		"aws load balancer controller installer factory is nil",
	)
	ErrAWSLoadBalancerControllerOwnershipReporterUnavailable = errors.New(
		"aws load balancer controller installer cannot report GitOps ownership",
	)
	ErrAWSLoadBalancerControllerIdentityReporterUnavailable = errors.New(
		"aws load balancer controller installer cannot report release identity",
	)
	ErrAWSLoadBalancerControllerReleaseIdentityMismatch = errors.New(
		"aws load balancer controller ownership is unresolved: release identity changed",
	)
	ErrAWSLoadBalancerControllerReleaseIdentityEmpty = errors.New(
		"aws load balancer controller release identity is empty",
	)
)

// InstallerFactories holds factory functions for creating component installers.
// These can be overridden in tests for dependency injection.
type InstallerFactories struct {
	Flux func(
		client helm.Interface,
		timeout time.Duration,
		operatorVersion string,
	) installer.Installer
	CertManager               func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	CSI                       func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	PolicyEngine              func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	ArgoCD                    func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	KubeletCSRApprover        func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	ClusterAutoscaler         func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error)
	AWSLoadBalancerController func(
		clusterCfg *v1alpha1.Cluster,
		ksailManaged bool,
	) (installer.Installer, error)
	// EnsureArgoCDResources configures default Argo CD resources post-install.
	EnsureArgoCDResources func(
		ctx context.Context, kubeconfig string, clusterCfg *v1alpha1.Cluster, clusterName string,
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
	// ClusterStabilityCheck waits for the API server and kube-system DaemonSets
	// to stabilize before/between installation phases. The bool argument
	// indicates whether CNI was just installed (skips the redundant node
	// readiness check). Set to nil to use the default waitForClusterStability.
	ClusterStabilityCheck func(ctx context.Context, clusterCfg *v1alpha1.Cluster, cniInstalled bool) error
	// NodeSchedulabilityWait waits for nodes to become schedulable after the
	// cloud-provider init pre-phase removes the uninitialized taint. Set to nil
	// to use the default waitForNodeSchedulability.
	NodeSchedulabilityWait func(ctx context.Context, clusterCfg *v1alpha1.Cluster) error
	// WaitForCSRApprover waits for the kubelet-serving-cert-approver deployment
	// (Talos inlineManifests) to be ready before infrastructure installs. Set to
	// nil to use the default waitForKubeletCSRApprover.
	WaitForCSRApprover func(ctx context.Context, clusterCfg *v1alpha1.Cluster) error
	// CloudProviderInitInstall installs the cloud controller manager
	// (hcloud-ccm) during the cloud-provider init pre-phase. Set to nil to use
	// the default InstallLoadBalancerSilent. The override only affects the
	// pre-phase; the normal parallel infra path uses InstallLoadBalancerSilent
	// directly.
	CloudProviderInitInstall silentInstallFunc
	// ReservedSandboxMonitor watches K3s-on-Docker Kubernetes events while
	// GitOps setup runs and returns k8s.ErrRepeatedReservedPodSandbox when
	// nested containerd repeatedly reserves a pod sandbox name. Set to nil to
	// use the default typed-event monitor.
	ReservedSandboxMonitor func(ctx context.Context, clusterCfg *v1alpha1.Cluster) error
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

		helmClient, kubeconfig, err := factories.HelmClientFactory(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)

		//nolint:exhaustive // PolicyEngineNone is handled above with early return
		switch engine {
		case v1alpha1.PolicyEngineKyverno:
			timeout = max(timeout, installer.KyvernoInstallTimeout)

			return kyvernoinstaller.NewInstaller(
				helmClient,
				timeout,
				kubeconfig,
				clusterCfg.Spec.Cluster.Connection.Context,
				installer.IsHAEnabled(clusterCfg.Spec.Cluster.TotalNodeCount()),
			), nil
		case v1alpha1.PolicyEngineGatekeeper:
			timeout = max(timeout, installer.GatekeeperInstallTimeout)

			return gatekeeperinstaller.NewInstaller(
				helmClient,
				kubeconfig,
				clusterCfg.Spec.Cluster.Connection.Context,
				timeout,
				installer.IsHAEnabled(clusterCfg.Spec.Cluster.TotalNodeCount()),
			), nil
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
			networkName := hcloudccminstaller.ResolveHetznerNetworkName(
				clusterCfg,
				resolveClusterNameFromContext(clusterCfg),
			)

			return hetznercsiinstaller.NewInstaller(
				helmClient,
				kubeconfig,
				clusterCfg.Spec.Cluster.Connection.Context,
				timeout,
				networkName,
				installer.IsHAEnabled(clusterCfg.Spec.Cluster.TotalNodeCount()),
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

// clusterAutoscalerFactory creates the Cluster Autoscaler factory function.
func clusterAutoscalerFactory(
	factories *InstallerFactories,
) func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	return func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, _, err := factories.HelmClientFactory(clusterCfg)
		if err != nil {
			return nil, err
		}

		timeout := installer.GetInstallTimeout(clusterCfg)
		haEnabled := installer.IsHAEnabled(clusterCfg.Spec.Cluster.TotalNodeCount())
		hetzner := clusterCfg.Spec.Provider.Hetzner

		return clusterautoscalerinstaller.NewInstaller(
			helmClient, timeout, clusterCfg.Spec.Cluster.Autoscaler.Node, haEnabled,
			hetzner.WorkerIPv4Enabled(), hetzner.WorkerIPv6Enabled(),
		)
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

// haHelmInstallerFactory creates a factory for Helm-based installers that
// derive haEnabled from the cluster node count. The constructor receives
// (helmClient, timeout, haEnabled).
func haHelmInstallerFactory(
	factories *InstallerFactories,
	newInstaller func(client helm.Interface, timeout time.Duration, haEnabled bool) installer.Installer,
	minTimeout time.Duration,
) func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
	return func(clusterCfg *v1alpha1.Cluster) (installer.Installer, error) {
		helmClient, timeout, err := resolveHelmClientAndTimeout(
			factories, clusterCfg, minTimeout,
		)
		if err != nil {
			return nil, err
		}

		haEnabled := installer.IsHAEnabled(clusterCfg.Spec.Cluster.TotalNodeCount())

		return newInstaller(helmClient, timeout, haEnabled), nil
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
			helmClient,
			timeout,
			sopsEnabled,
			installer.IsHAEnabled(clusterCfg.Spec.Cluster.TotalNodeCount()),
		), nil
	}
}

// DefaultInstallerFactories returns the default installer factories.
func DefaultInstallerFactories() *InstallerFactories {
	factories := &InstallerFactories{}

	// Set HelmClientFactory first as other factories depend on it
	factories.HelmClientFactory = HelmClientForCluster

	factories.Flux = func(
		client helm.Interface,
		timeout time.Duration,
		operatorVersion string,
	) installer.Installer {
		return fluxinstaller.NewInstaller(client, timeout, operatorVersion)
	}

	factories.CertManager = haHelmInstallerFactory(
		factories,
		func(c helm.Interface, t time.Duration, ha bool) installer.Installer {
			return certmanagerinstaller.NewInstaller(c, t, ha)
		},
		installer.CertManagerInstallTimeout,
	)
	factories.ArgoCD = argoCDInstallerFactory(factories)
	factories.KubeletCSRApprover = haHelmInstallerFactory(
		factories,
		func(c helm.Interface, t time.Duration, ha bool) installer.Installer {
			return kubeletcsrapproverinstaller.NewInstaller(c, t, ha)
		},
		0,
	)
	factories.CSI = csiFactory(factories)
	factories.PolicyEngine = policyEngineFactory(factories)
	factories.ClusterAutoscaler = clusterAutoscalerFactory(factories)
	factories.AWSLoadBalancerController = func(
		clusterCfg *v1alpha1.Cluster,
		ksailManaged bool,
	) (installer.Installer, error) {
		return newEKSLoadBalancerInstaller(clusterCfg, factories, ksailManaged)
	}

	factories.EnsureArgoCDResources = EnsureArgoCDResources
	factories.SetupFluxInstance = fluxinstaller.SetupInstance
	factories.WaitForFluxReady = fluxinstaller.WaitForFluxReady

	factories.ClusterStabilityCheck = waitForClusterStability
	factories.NodeSchedulabilityWait = waitForNodeSchedulability
	factories.WaitForCSRApprover = waitForKubeletCSRApprover
	factories.CloudProviderInitInstall = InstallLoadBalancerSilent

	return factories
}
