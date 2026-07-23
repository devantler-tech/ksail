package setup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	awslbcontrollerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/awslbcontroller"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cloudproviderkind"
	hcloudccminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hcloudccm"
	metallbinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metallb"
	metricsserverinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metricsserver"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
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
	err := k8s.ValidateContextExists(kubeconfigPath, contextName)
	if err != nil {
		return fmt.Errorf("validate kubeconfig context: %w", err)
	}

	return nil
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
// Special cases:
//   - Talos × Hetzner: hcloud-ccm is not pre-installed and must be installed
//     by KSail when LoadBalancer is either Default or Enabled.
//   - EKS: the AWS Load Balancer Controller is an explicit experimental
//     opt-in (spec.cluster.eks.experimentalAWSLoadBalancerController) on top
//     of LoadBalancer Enabled; without it EKS keeps its default in-tree
//     Classic Load Balancer path and nothing is installed.
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

	// Special handling for EKS: install the AWS Load Balancer Controller only
	// on the explicit experimental opt-in, and only when LoadBalancer is
	// explicitly Enabled (Default keeps the in-tree path untouched).
	if dist == v1alpha1.DistributionEKS {
		return lbSetting == v1alpha1.LoadBalancerEnabled &&
			clusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController
	}

	// Generic behavior for all other distribution × provider combinations.
	if lbSetting != v1alpha1.LoadBalancerEnabled {
		return false
	}

	// Don't install if distribution × provider provides it by default.
	return !dist.ProvidesLoadBalancerByDefault(provider)
}

// NeedsClusterAutoscalerInstall determines if the Cluster Autoscaler needs to be installed.
// The Cluster Autoscaler is only supported for Talos clusters on Hetzner Cloud
// with node autoscaling explicitly enabled.
func NeedsClusterAutoscalerInstall(clusterCfg *v1alpha1.Cluster) bool {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos {
		return false
	}

	if clusterCfg.Spec.Cluster.Provider != v1alpha1.ProviderHetzner {
		return false
	}

	return clusterCfg.Spec.Cluster.Autoscaler.Node.Enabled.IsEnabled() ||
		clusterCfg.Spec.Cluster.NodeAutoscaling == v1alpha1.NodeAutoscalingEnabled
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

	msInstaller := metricsserverinstaller.NewInstaller(
		helmClient,
		timeout,
		clusterCfg.Spec.Cluster.Distribution,
		installer.IsHAEnabled(clusterCfg.Spec.Cluster.TotalNodeCount()),
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
// For EKS, installs the AWS Load Balancer Controller on the experimental opt-in.
func InstallLoadBalancerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return installCloudProviderKind(ctx, clusterCfg)
	case v1alpha1.DistributionTalos:
		return installTalosLoadBalancer(ctx, clusterCfg, factories)
	case v1alpha1.DistributionK3s:
		// K3s already has ServiceLB (Klipper) by default, no installation needed
		return nil
	case v1alpha1.DistributionEKS:
		// Default: EKS keeps its in-tree Classic Load Balancer path; the AWS
		// Load Balancer Controller is installed only on the experimental opt-in.
		return installEKSLoadBalancer(ctx, clusterCfg, factories)
	case v1alpha1.DistributionVCluster, v1alpha1.DistributionKWOK,
		v1alpha1.DistributionGKE, v1alpha1.DistributionAKS:
		// VCluster (Vind) handles LoadBalancer via its own networking.
		// KWOK is a simulation cluster with no real network dataplane.
		// GKE relies on GCP's built-in cloud load balancing.
		// AKS relies on Azure's built-in cloud load balancing.
		return nil
	}

	return nil
}

// installEKSLoadBalancer installs the AWS Load Balancer Controller when the
// experimental opt-in is set together with LoadBalancer Enabled; otherwise EKS
// keeps its default in-tree path and nothing is installed. The chart's
// required clusterName value is resolved the same way the config manager
// resolves it (eksctl config file first, kubeconfig context as fallback).
func installEKSLoadBalancer(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	_, _, err := InstallEKSLoadBalancerControllerWithResult(ctx, clusterCfg, factories)

	return err
}

type installResultReporter interface {
	InstallWithResult(ctx context.Context) (mutated bool, err error)
}

type gitOpsOwnershipReporter interface {
	IsGitOpsManaged(ctx context.Context) (managed bool, err error)
}

type releaseIdentityReporter interface {
	ReleaseIdentity(ctx context.Context) (string, error)
}

type releaseIdentityOwnershipReporter interface {
	OwnsReleaseIdentity(ctx context.Context, expected string) (bool, error)
}

// InstallEKSLoadBalancerControllerWithResult installs or upgrades the controller and reports
// whether Helm was actually mutated. GitOps-owned releases are successful, unmodified skips.
func InstallEKSLoadBalancerControllerWithResult(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) (bool, string, error) {
	var releaseIdentity string

	result, err := withRequiredEKSLoadBalancerInstaller(clusterCfg, factories, func(
		lbInstaller installer.Installer,
	) (bool, error) {
		mutated, installErr := installWithResult(ctx, lbInstaller)
		if installErr != nil {
			return false, fmt.Errorf(
				"aws-load-balancer-controller installation failed: %w",
				installErr,
			)
		}

		if !mutated {
			return false, nil
		}

		identity, identityErr := loadBalancerControllerReleaseIdentity(ctx, lbInstaller)
		if identityErr != nil {
			return false, identityErr
		}

		releaseIdentity = identity

		return true, nil
	})

	return result, releaseIdentity, err
}

func installWithResult(ctx context.Context, component installer.Installer) (bool, error) {
	resultReporter, reportsResult := component.(installResultReporter)
	if reportsResult {
		mutated, err := resultReporter.InstallWithResult(ctx)
		if err != nil {
			return false, fmt.Errorf("install component with result: %w", err)
		}

		return mutated, nil
	}

	installErr := component.Install(ctx)
	if installErr != nil {
		return false, fmt.Errorf("install component: %w", installErr)
	}

	return true, nil
}

func withRequiredEKSLoadBalancerInstaller(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	action func(installer.Installer) (bool, error),
) (bool, error) {
	if !NeedsLoadBalancerInstall(clusterCfg) {
		return false, nil
	}

	lbInstaller, err := createEKSLoadBalancerInstaller(clusterCfg, factories, false)
	if err != nil {
		return false, err
	}

	result, err := action(lbInstaller)
	if err != nil {
		return false, fmt.Errorf("run required EKS load balancer controller action: %w", err)
	}

	return result, nil
}

// EKSLoadBalancerControllerManagedByKSail verifies the ownership established by a successful
// create/recreate workflow. A GitOps-owned release was deliberately skipped and remains unowned.
func EKSLoadBalancerControllerManagedByKSail(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) (bool, string, error) {
	if !NeedsLoadBalancerInstall(clusterCfg) {
		return false, "", nil
	}

	lbInstaller, err := createEKSLoadBalancerInstaller(clusterCfg, factories, false)
	if err != nil {
		return false, "", err
	}

	reporter, reportsOwnership := lbInstaller.(gitOpsOwnershipReporter)
	if !reportsOwnership {
		return false, "", ErrAWSLoadBalancerControllerOwnershipReporterUnavailable
	}

	gitOpsManaged, err := reporter.IsGitOpsManaged(ctx)
	if err != nil {
		return false, "", fmt.Errorf(
			"verify AWS load balancer controller ownership after creation: %w",
			err,
		)
	}

	if gitOpsManaged {
		return false, "", nil
	}

	identity, err := loadBalancerControllerReleaseIdentity(ctx, lbInstaller)
	if err != nil {
		return false, "", err
	}

	return true, identity, nil
}

func loadBalancerControllerReleaseIdentity(
	ctx context.Context,
	lbInstaller installer.Installer,
) (string, error) {
	reporter, reportsIdentity := lbInstaller.(releaseIdentityReporter)
	if !reportsIdentity {
		return "", ErrAWSLoadBalancerControllerIdentityReporterUnavailable
	}

	identity, err := reporter.ReleaseIdentity(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve AWS load balancer controller release identity: %w", err)
	}

	identity = strings.TrimSpace(identity)
	if identity == "" {
		return "", ErrAWSLoadBalancerControllerReleaseIdentityEmpty
	}

	return identity, nil
}

// UninstallEKSLoadBalancerControllerSilent removes the AWS Load Balancer Controller
// only when the live release identity still matches the persisted KSail-owned incarnation.
func UninstallEKSLoadBalancerControllerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	expectedReleaseIdentity string,
) error {
	lbInstaller, err := createEKSLoadBalancerInstaller(clusterCfg, factories, true)
	if err != nil {
		return err
	}

	matches, err := loadBalancerControllerOwnsReleaseIdentity(
		ctx,
		lbInstaller,
		expectedReleaseIdentity,
	)
	if err != nil {
		return err
	}

	if !matches {
		return ErrAWSLoadBalancerControllerReleaseIdentityMismatch
	}

	uninstallErr := lbInstaller.Uninstall(ctx)
	if uninstallErr != nil {
		return fmt.Errorf("aws-load-balancer-controller uninstall failed: %w", uninstallErr)
	}

	return nil
}

func loadBalancerControllerOwnsReleaseIdentity(
	ctx context.Context,
	lbInstaller installer.Installer,
	expectedReleaseIdentity string,
) (bool, error) {
	expectedReleaseIdentity = strings.TrimSpace(expectedReleaseIdentity)
	if expectedReleaseIdentity == "" {
		return false, ErrAWSLoadBalancerControllerReleaseIdentityEmpty
	}

	if reporter, ok := lbInstaller.(releaseIdentityOwnershipReporter); ok {
		matches, err := reporter.OwnsReleaseIdentity(ctx, expectedReleaseIdentity)
		if err != nil {
			return false, fmt.Errorf(
				"verify AWS load balancer controller release identity history: %w",
				err,
			)
		}

		return matches, nil
	}

	liveReleaseIdentity, err := loadBalancerControllerReleaseIdentity(ctx, lbInstaller)
	if err != nil {
		return false, err
	}

	return liveReleaseIdentity == expectedReleaseIdentity, nil
}

func createEKSLoadBalancerInstaller(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	ksailManaged bool,
) (installer.Installer, error) {
	if factories.AWSLoadBalancerController == nil {
		return nil, ErrAWSLoadBalancerControllerInstallerFactoryNil
	}

	return factories.AWSLoadBalancerController(clusterCfg, ksailManaged)
}

func newEKSLoadBalancerInstaller(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	ksailManaged bool,
) (installer.Installer, error) {
	clusterName, fileRegion, nameFromConfig, err := ksailconfigmanager.ResolveEKSClusterMetadata(
		clusterCfg,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve EKS cluster metadata: %w", err)
	}

	// Honor the same region precedence as the create path: the environment
	// variable named by spec.provider.aws.regionEnvVar (default AWS_REGION)
	// overrides eks.yaml, whose region is target-bound only when the file also
	// named the cluster.
	region := lifecycle.ResolveAWSRegion(
		clusterCfg.Spec.Provider.AWS,
		&clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{
				Region:         fileRegion,
				NameFromConfig: nameFromConfig,
			},
		},
	)

	helmClient, _, timeout, err := helmClientSetup(clusterCfg, factories)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to setup helm client for aws-load-balancer-controller: %w",
			err,
		)
	}

	haEnabled := installer.IsHAEnabled(clusterCfg.Spec.Cluster.TotalNodeCount())

	lbInstaller, err := awslbcontrollerinstaller.NewInstaller(
		helmClient,
		timeout,
		clusterName,
		region,
		clusterCfg.Spec.Cluster.EKS.AWSLoadBalancerControllerServiceAccount,
		haEnabled,
		ksailManaged,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to configure aws-load-balancer-controller installer: %w",
			err,
		)
	}

	return lbInstaller, nil
}

// installTalosLoadBalancer installs the provider-appropriate load balancer for
// the Talos distribution: MetalLB on Docker, hcloud CCM on Hetzner; the other
// providers either manage load balancing themselves or do not support Talos.
func installTalosLoadBalancer(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
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
	case v1alpha1.ProviderGCP:
		// GCP is not a supported provider for Talos.
		return nil
	case v1alpha1.ProviderAzure:
		// Azure is not a supported provider for Talos.
		return nil
	case v1alpha1.ProviderKubernetes:
		// Kubernetes provider: no additional load balancer needed.
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

	networkName := hcloudccminstaller.ResolveHetznerNetworkName(
		clusterCfg,
		resolveClusterNameFromContext(clusterCfg),
	)

	ccmInstaller := hcloudccminstaller.NewInstaller(
		helmClient,
		kubeconfigPath,
		clusterCfg.Spec.Cluster.Connection.Context,
		timeout,
		networkName,
		installer.IsHAEnabled(clusterCfg.Spec.Cluster.TotalNodeCount()),
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

// InstallClusterAutoscalerSilent installs the Cluster Autoscaler silently for parallel execution.
func InstallClusterAutoscalerSilent(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
) error {
	return installFromFactory(
		ctx, clusterCfg, factories.ClusterAutoscaler,
		ErrClusterAutoscalerInstallerFactoryNil, "cluster-autoscaler",
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
