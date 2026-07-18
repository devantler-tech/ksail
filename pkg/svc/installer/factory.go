package installer

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	argocdinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/argocd"
	awslbcontrollerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/awslbcontroller"
	certmanagerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/certmanager"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cloudproviderkind"
	clusterautoscalerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/clusterautoscaler"
	calicoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/calico"
	ciliuminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/cilium"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	gatekeeperinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/gatekeeper"
	hcloudccminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hcloudccm"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hetznercsi"
	kubeletcsrapproverinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/kubeletcsrapprover"
	kyvernoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/kyverno"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/localpathstorage"
	metallbinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metallb"
	metricsserverinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metricsserver"
)

// Factory creates installers based on cluster configuration.
// It holds the shared dependencies required by installers.
type Factory struct {
	helmClient   helm.Interface
	dockerClient dockerclient.Client
	kubeconfig   string
	kubecontext  string
	timeout      time.Duration
	distribution v1alpha1.Distribution
	// eksClusterName is the provisioned EKS cluster name, required by the AWS
	// Load Balancer Controller chart. Only callers that know the provisioned
	// name (e.g. the operator) can supply it — see WithEKSClusterName.
	eksClusterName string
}

// Option configures optional Factory dependencies.
type Option func(*Factory)

// WithEKSClusterName supplies the provisioned EKS cluster name, which the AWS
// Load Balancer Controller chart requires as its clusterName value.
func WithEKSClusterName(name string) Option {
	return func(f *Factory) {
		f.eksClusterName = name
	}
}

// NewFactory creates a new installer factory with the required dependencies.
func NewFactory(
	helmClient helm.Interface,
	dockerClient dockerclient.Client,
	kubeconfig, kubecontext string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
	opts ...Option,
) *Factory {
	factory := &Factory{
		helmClient:   helmClient,
		dockerClient: dockerClient,
		kubeconfig:   kubeconfig,
		kubecontext:  kubecontext,
		timeout:      timeout,
		distribution: distribution,
	}

	for _, opt := range opts {
		opt(factory)
	}

	return factory
}

// CreateInstallersForConfig creates installers for all components specified in the cluster config.
// Returns a map of component name to installer.
func (f *Factory) CreateInstallersForConfig(cfg *v1alpha1.Cluster) (map[string]Installer, error) {
	installers := make(map[string]Installer)
	spec := cfg.Spec.Cluster
	haEnabled := IsHAEnabled(spec.TotalNodeCount())

	f.addGitOpsInstaller(installers, spec, cfg.Spec.Workload.Flux.OperatorVersion, haEnabled)
	f.addCNIInstaller(installers, spec, haEnabled)
	f.addPolicyEngineInstaller(installers, spec, haEnabled)
	f.addCertManagerInstaller(installers, spec, haEnabled)
	f.addMetricsServerInstaller(installers, spec, haEnabled)
	f.addCSIInstallers(installers, cfg, haEnabled)

	err := f.addLoadBalancerInstaller(installers, cfg, haEnabled)
	if err != nil {
		return nil, err
	}

	err = f.addClusterAutoscalerInstaller(installers, spec, cfg.Spec.Provider.Hetzner, haEnabled)
	if err != nil {
		return nil, err
	}

	return installers, nil
}

// GetImagesFromInstallers retrieves container images from all provided installers.
// Returns a deduplicated list of all images across all installers.
func GetImagesFromInstallers(
	ctx context.Context,
	installers map[string]Installer,
) ([]string, error) {
	seen := make(map[string]struct{})

	var result []string

	for name, inst := range installers {
		images, err := inst.Images(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get images for %s: %w", name, err)
		}

		for _, img := range images {
			if _, exists := seen[img]; !exists {
				seen[img] = struct{}{}
				result = append(result, img)
			}
		}
	}

	return result, nil
}

// GetImagesForCluster is a convenience function that creates installers and retrieves images
// for a given cluster configuration.
func (f *Factory) GetImagesForCluster(
	ctx context.Context,
	cfg *v1alpha1.Cluster,
) ([]string, error) {
	installers, err := f.CreateInstallersForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create installers: %w", err)
	}

	return GetImagesFromInstallers(ctx, installers)
}

func (f *Factory) addGitOpsInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
	operatorVersion string,
	haEnabled bool,
) {
	switch spec.GitOpsEngine {
	case v1alpha1.GitOpsEngineFlux:
		installers["flux"] = fluxinstaller.NewInstaller(
			f.helmClient,
			max(f.timeout, FluxInstallTimeout),
			operatorVersion,
		)
	case v1alpha1.GitOpsEngineArgoCD:
		installers["argocd"] = argocdinstaller.NewInstaller(
			f.helmClient, f.timeout, argocdinstaller.ShouldEnableSOPS(spec.SOPS), haEnabled,
		)
	case v1alpha1.GitOpsEngineNone:
		// No GitOps engine configured
	}
}

func (f *Factory) addCNIInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
	haEnabled bool,
) {
	switch spec.CNI {
	case v1alpha1.CNICilium:
		installers["cilium"] = ciliuminstaller.NewInstaller(
			f.helmClient, f.kubeconfig, f.kubecontext, f.timeout,
			f.distribution, spec.Provider, spec.LoadBalancer, haEnabled,
		)
	case v1alpha1.CNICalico:
		installers["calico"] = calicoinstaller.NewInstaller(
			f.helmClient, f.kubeconfig, f.kubecontext,
			max(f.timeout, CalicoInstallTimeout), f.distribution, haEnabled,
		)
	case v1alpha1.CNIDefault:
		// Default CNI - no explicit installer needed
	}
}

func (f *Factory) addPolicyEngineInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
	haEnabled bool,
) {
	switch spec.PolicyEngine {
	case v1alpha1.PolicyEngineKyverno:
		installers["kyverno"] = kyvernoinstaller.NewInstaller(
			f.helmClient,
			max(f.timeout, KyvernoInstallTimeout),
			f.kubeconfig,
			f.kubecontext,
			haEnabled,
		)
	case v1alpha1.PolicyEngineGatekeeper:
		installers["gatekeeper"] = gatekeeperinstaller.NewInstaller(
			f.helmClient,
			f.kubeconfig,
			f.kubecontext,
			max(f.timeout, GatekeeperInstallTimeout),
			haEnabled,
		)
	case v1alpha1.PolicyEngineNone:
		// No policy engine configured
	}
}

func (f *Factory) addCertManagerInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
	haEnabled bool,
) {
	if spec.CertManager == v1alpha1.CertManagerEnabled {
		installers["cert-manager"] = certmanagerinstaller.NewInstaller(
			f.helmClient, max(f.timeout, CertManagerInstallTimeout), haEnabled,
		)
	}
}

func (f *Factory) addMetricsServerInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
	haEnabled bool,
) {
	if spec.MetricsServer == v1alpha1.MetricsServerEnabled ||
		(spec.MetricsServer == v1alpha1.MetricsServerDefault &&
			!spec.Distribution.ProvidesMetricsServerByDefault()) {
		installers["metrics-server"] = metricsserverinstaller.NewInstaller(
			f.helmClient, f.timeout, f.distribution, haEnabled,
		)
	}
}

func (f *Factory) addCSIInstallers(
	installers map[string]Installer,
	cfg *v1alpha1.Cluster,
	haEnabled bool,
) {
	spec := cfg.Spec.Cluster

	if f.needsLocalPathStorage(spec) {
		installers["local-path-storage"] = localpathstorageinstaller.NewInstaller(
			f.kubeconfig, f.kubecontext, f.timeout, f.distribution,
		)
	}

	if f.needsHetznerCSI(spec) {
		clusterName := hcloudccminstaller.ExtractClusterNameFromTalosContext(f.kubecontext)
		if clusterName == "" {
			clusterName = f.distribution.DefaultClusterName()
		}

		networkName := hcloudccminstaller.ResolveHetznerNetworkName(cfg, clusterName)
		installers["hetzner-csi"] = hetznercsiinstaller.NewInstaller(
			f.helmClient, f.kubeconfig, f.kubecontext, f.timeout, networkName, haEnabled,
		)
		installers["kubelet-csr-approver"] = kubeletcsrapproverinstaller.NewInstaller(
			f.helmClient,
			f.timeout,
			haEnabled,
		)
	}
}

func (f *Factory) addLoadBalancerInstaller(
	installers map[string]Installer,
	cfg *v1alpha1.Cluster,
	haEnabled bool,
) error {
	spec := cfg.Spec.Cluster

	if f.needsCloudProviderKind(spec) && f.dockerClient != nil {
		installers["cloud-provider-kind"] = cloudproviderkindinstaller.NewInstaller(
			f.dockerClient,
		)
	}

	if f.needsMetalLB(spec) {
		installers["metallb"] = metallbinstaller.NewInstaller(
			f.helmClient,
			f.kubeconfig,
			f.kubecontext,
			f.timeout,
			"", // Use default IP range
		)
	}

	if f.needsHcloudCCM(spec) {
		clusterName := hcloudccminstaller.ExtractClusterNameFromTalosContext(f.kubecontext)
		if clusterName == "" {
			clusterName = f.distribution.DefaultClusterName()
		}

		networkName := hcloudccminstaller.ResolveHetznerNetworkName(cfg, clusterName)
		installers["hcloud-ccm"] = hcloudccminstaller.NewInstaller(
			f.helmClient,
			f.kubeconfig,
			f.kubecontext,
			f.timeout,
			networkName,
			haEnabled,
		)
	}

	if f.needsAWSLBController(spec) {
		// Fail loud rather than silently skipping: the user explicitly opted
		// in, so an unknowable cluster name is a wiring bug, not a no-op.
		awslbc, err := awslbcontrollerinstaller.NewInstaller(
			f.helmClient,
			f.timeout,
			f.eksClusterName,
			"", // region: rely on the chart's own discovery
			haEnabled,
		)
		if err != nil {
			return fmt.Errorf(
				"experimental AWS Load Balancer Controller is enabled but unusable "+
					"(construct the factory with WithEKSClusterName): %w",
				err,
			)
		}

		installers["aws-load-balancer-controller"] = awslbc
	}

	return nil
}

// needsAWSLBController determines if the AWS Load Balancer Controller is
// needed. It is an experimental opt-in for EKS clusters: LoadBalancer must be
// explicitly Enabled AND spec.cluster.eks.experimentalAWSLoadBalancerController
// set, otherwise EKS keeps its default in-tree Classic Load Balancer path and
// nothing is installed.
func (f *Factory) needsAWSLBController(spec v1alpha1.ClusterSpec) bool {
	if spec.Distribution != v1alpha1.DistributionEKS {
		return false
	}

	if spec.LoadBalancer != v1alpha1.LoadBalancerEnabled {
		return false
	}

	return spec.EKS.ExperimentalAWSLoadBalancerController
}

// needsLocalPathStorage determines if local-path-storage is needed.
func (f *Factory) needsLocalPathStorage(spec v1alpha1.ClusterSpec) bool {
	// K3s has built-in storage
	if spec.Distribution == v1alpha1.DistributionK3s {
		return false
	}

	// Talos with Hetzner uses Hetzner CSI
	if spec.Distribution == v1alpha1.DistributionTalos &&
		spec.Provider == v1alpha1.ProviderHetzner {
		return false
	}

	return spec.CSI == v1alpha1.CSIEnabled
}

// needsHetznerCSI determines if Hetzner CSI is needed.
func (f *Factory) needsHetznerCSI(spec v1alpha1.ClusterSpec) bool {
	if spec.Distribution != v1alpha1.DistributionTalos {
		return false
	}

	if spec.Provider != v1alpha1.ProviderHetzner {
		return false
	}

	return spec.CSI != v1alpha1.CSIDisabled
}

// needsCloudProviderKind determines if cloud-provider-kind is needed.
func (f *Factory) needsCloudProviderKind(spec v1alpha1.ClusterSpec) bool {
	if spec.Distribution != v1alpha1.DistributionVanilla {
		return false
	}

	return spec.LoadBalancer == v1alpha1.LoadBalancerEnabled
}

// needsMetalLB determines if MetalLB is needed.
// MetalLB provides LoadBalancer support for Talos clusters running on Docker.
func (f *Factory) needsMetalLB(spec v1alpha1.ClusterSpec) bool {
	if spec.Distribution != v1alpha1.DistributionTalos {
		return false
	}

	if spec.Provider != v1alpha1.ProviderDocker {
		return false
	}

	return spec.LoadBalancer == v1alpha1.LoadBalancerEnabled
}

// needsHcloudCCM determines if Hetzner Cloud Controller Manager is needed.
// hcloud-ccm provides LoadBalancer support for Talos clusters running on Hetzner Cloud,
// and is also required for CSI topology labeling: the CCM applies the
// `instance.hetzner.cloud/provided-by` node label that the hcloud-csi driver
// reads at start-up to register topology segments. Therefore CCM must be
// installed whenever Hetzner CSI is enabled.
func (f *Factory) needsHcloudCCM(spec v1alpha1.ClusterSpec) bool {
	if spec.Distribution != v1alpha1.DistributionTalos {
		return false
	}

	if spec.Provider != v1alpha1.ProviderHetzner {
		return false
	}

	return spec.LoadBalancer == v1alpha1.LoadBalancerDefault ||
		spec.LoadBalancer == v1alpha1.LoadBalancerEnabled ||
		spec.CSI != v1alpha1.CSIDisabled
}

func (f *Factory) addClusterAutoscalerInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
	hetzner v1alpha1.OptionsHetzner,
	haEnabled bool,
) error {
	if !f.needsClusterAutoscaler(spec) {
		return nil
	}

	// Autoscaler-created nodes are workers; they inherit the worker public-net
	// setting cluster-wide (the Hetzner cluster-autoscaler has no per-pool knob).
	inst, err := clusterautoscalerinstaller.NewInstaller(
		f.helmClient, f.timeout, spec.Autoscaler.Node, haEnabled,
		hetzner.WorkerIPv4Enabled(), hetzner.WorkerIPv6Enabled(),
	)
	if err != nil {
		return fmt.Errorf("failed to create cluster-autoscaler installer: %w", err)
	}

	installers["cluster-autoscaler"] = inst

	return nil
}

// needsClusterAutoscaler determines if the Cluster Autoscaler is needed.
// Cluster Autoscaler is only supported for Talos clusters on Hetzner Cloud
// with node autoscaling explicitly enabled.
func (f *Factory) needsClusterAutoscaler(spec v1alpha1.ClusterSpec) bool {
	autoscalingEnabled := spec.Autoscaler.Node.Enabled.IsEnabled() ||
		spec.NodeAutoscaling == v1alpha1.NodeAutoscalingEnabled

	return spec.Distribution == v1alpha1.DistributionTalos &&
		spec.Provider == v1alpha1.ProviderHetzner &&
		autoscalingEnabled
}
