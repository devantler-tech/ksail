package installer

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	argocdinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/argocd"
	certmanagerinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/certmanager"
	cloudproviderkindinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cloudproviderkind"
	calicoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/calico"
	ciliuminstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/cni/cilium"
	fluxinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/flux"
	gatekeeperinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/gatekeeper"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/hetznercsi"
	kubeletcsrapproverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/kubeletcsrapprover"
	kyvernoinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/kyverno"
	localpathstorageinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/localpathstorage"
	metricsserverinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/metricsserver"
	"github.com/docker/docker/client"
)

// Factory creates installers based on cluster configuration.
// It holds the shared dependencies required by installers.
type Factory struct {
	helmClient   helm.Interface
	dockerClient client.APIClient
	kubeconfig   string
	kubecontext  string
	timeout      time.Duration
	distribution v1alpha1.Distribution
}

// NewFactory creates a new installer factory with the required dependencies.
func NewFactory(
	helmClient helm.Interface,
	dockerClient client.APIClient,
	kubeconfig, kubecontext string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
) *Factory {
	return &Factory{
		helmClient:   helmClient,
		dockerClient: dockerClient,
		kubeconfig:   kubeconfig,
		kubecontext:  kubecontext,
		timeout:      timeout,
		distribution: distribution,
	}
}

// CreateInstallersForConfig creates installers for all components specified in the cluster config.
// Returns a map of component name to installer.
func (f *Factory) CreateInstallersForConfig(cfg *v1alpha1.Cluster) map[string]Installer {
	installers := make(map[string]Installer)
	spec := cfg.Spec.Cluster

	f.addGitOpsInstaller(installers, spec)
	f.addCNIInstaller(installers, spec)
	f.addPolicyEngineInstaller(installers, spec)
	f.addCertManagerInstaller(installers, spec)
	f.addMetricsServerInstaller(installers, spec)
	f.addCSIInstallers(installers, spec)
	f.addLoadBalancerInstaller(installers, spec)

	return installers
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
	installers := f.CreateInstallersForConfig(cfg)

	return GetImagesFromInstallers(ctx, installers)
}

func (f *Factory) addGitOpsInstaller(installers map[string]Installer, spec v1alpha1.ClusterSpec) {
	switch spec.GitOpsEngine {
	case v1alpha1.GitOpsEngineFlux:
		installers["flux"] = fluxinstaller.NewInstaller(
			f.helmClient,
			max(f.timeout, FluxInstallTimeout),
		)
	case v1alpha1.GitOpsEngineArgoCD:
		installers["argocd"] = argocdinstaller.NewInstaller(f.helmClient, f.timeout)
	case v1alpha1.GitOpsEngineNone:
		// No GitOps engine configured
	}
}

func (f *Factory) addCNIInstaller(installers map[string]Installer, spec v1alpha1.ClusterSpec) {
	switch spec.CNI {
	case v1alpha1.CNICilium:
		installers["cilium"] = ciliuminstaller.NewInstallerWithDistribution(
			f.helmClient, f.kubeconfig, f.kubecontext, f.timeout, f.distribution,
		)
	case v1alpha1.CNICalico:
		installers["calico"] = calicoinstaller.NewInstallerWithDistribution(
			f.helmClient, f.kubeconfig, f.kubecontext,
			max(f.timeout, CalicoInstallTimeout), f.distribution,
		)
	case v1alpha1.CNIDefault:
		// Default CNI - no explicit installer needed
	}
}

func (f *Factory) addPolicyEngineInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
) {
	switch spec.PolicyEngine {
	case v1alpha1.PolicyEngineKyverno:
		installers["kyverno"] = kyvernoinstaller.NewInstaller(
			f.helmClient, max(f.timeout, KyvernoInstallTimeout),
		)
	case v1alpha1.PolicyEngineGatekeeper:
		installers["gatekeeper"] = gatekeeperinstaller.NewInstaller(
			f.helmClient,
			f.timeout,
		)
	case v1alpha1.PolicyEngineNone:
		// No policy engine configured
	}
}

func (f *Factory) addCertManagerInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
) {
	if spec.CertManager == v1alpha1.CertManagerEnabled {
		installers["cert-manager"] = certmanagerinstaller.NewInstaller(
			f.helmClient, max(f.timeout, CertManagerInstallTimeout),
		)
	}
}

func (f *Factory) addMetricsServerInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
) {
	if spec.MetricsServer == v1alpha1.MetricsServerEnabled ||
		(spec.MetricsServer == v1alpha1.MetricsServerDefault &&
			!spec.Distribution.ProvidesMetricsServerByDefault()) {
		installers["metrics-server"] = metricsserverinstaller.NewInstallerWithDistribution(
			f.helmClient, f.timeout, f.distribution,
		)
	}
}

func (f *Factory) addCSIInstallers(installers map[string]Installer, spec v1alpha1.ClusterSpec) {
	if f.needsLocalPathStorage(spec) {
		installers["local-path-storage"] = localpathstorageinstaller.NewInstaller(
			f.kubeconfig, f.kubecontext, f.timeout, f.distribution,
		)
	}

	if f.needsHetznerCSI(spec) {
		installers["hetzner-csi"] = hetznercsiinstaller.NewInstaller(
			f.helmClient, f.kubeconfig, f.kubecontext, f.timeout,
		)
		installers["kubelet-csr-approver"] = kubeletcsrapproverinstaller.NewInstaller(
			f.helmClient,
			f.timeout,
		)
	}
}

func (f *Factory) addLoadBalancerInstaller(
	installers map[string]Installer,
	spec v1alpha1.ClusterSpec,
) {
	if f.needsCloudProviderKind(spec) && f.dockerClient != nil {
		installers["cloud-provider-kind"] = cloudproviderkindinstaller.NewInstaller(
			f.dockerClient,
		)
	}
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
