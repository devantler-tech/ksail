package detector

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ComponentDetector detects installed KSail components by querying the
// Kubernetes API (Helm releases, Deployments) and Docker daemon.
type ComponentDetector struct {
	helmClient   helm.Interface
	k8sClientset kubernetes.Interface
	dockerClient dockerclient.APIClient
}

// NewComponentDetector creates a detector with the required clients.
// dockerClient may be nil for non-Docker providers.
func NewComponentDetector(
	helmClient helm.Interface,
	k8sClientset kubernetes.Interface,
	dockerClient dockerclient.APIClient,
) *ComponentDetector {
	return &ComponentDetector{
		helmClient:   helmClient,
		k8sClientset: k8sClientset,
		dockerClient: dockerClient,
	}
}

// DetectComponents probes the running cluster to populate a ClusterSpec that
// reflects the actual installed components. Distribution and provider are set
// from the caller's known values; all other fields are detected.
func (d *ComponentDetector) DetectComponents(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
) (*v1alpha1.ClusterSpec, error) {
	spec := &v1alpha1.ClusterSpec{
		Distribution: distribution,
		Provider:     provider,
	}

	var err error

	spec.CNI, err = d.detectCNI(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect CNI: %w", err)
	}

	spec.CSI, err = d.detectCSI(ctx, distribution, provider)
	if err != nil {
		return nil, fmt.Errorf("detect CSI: %w", err)
	}

	spec.MetricsServer, err = d.detectMetricsServer(ctx, distribution)
	if err != nil {
		return nil, fmt.Errorf("detect MetricsServer: %w", err)
	}

	spec.LoadBalancer, err = d.detectLoadBalancer(ctx, distribution, provider)
	if err != nil {
		return nil, fmt.Errorf("detect LoadBalancer: %w", err)
	}

	spec.CertManager, err = d.detectCertManager(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect CertManager: %w", err)
	}

	spec.PolicyEngine, err = d.detectPolicyEngine(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect PolicyEngine: %w", err)
	}

	spec.GitOpsEngine, err = d.detectGitOpsEngine(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect GitOpsEngine: %w", err)
	}

	return spec, nil
}

// detectCNI probes for Cilium or Calico Helm releases.
func (d *ComponentDetector) detectCNI(ctx context.Context) (v1alpha1.CNI, error) {
	cilium, err := d.helmClient.ReleaseExists(ctx, ReleaseCilium, NamespaceCilium)
	if err != nil {
		return v1alpha1.CNIDefault, fmt.Errorf("check cilium release: %w", err)
	}

	if cilium {
		return v1alpha1.CNICilium, nil
	}

	calico, err := d.helmClient.ReleaseExists(ctx, ReleaseCalico, NamespaceCalico)
	if err != nil {
		return v1alpha1.CNIDefault, fmt.Errorf("check calico release: %w", err)
	}

	if calico {
		return v1alpha1.CNICalico, nil
	}

	return v1alpha1.CNIDefault, nil
}

// detectCSI determines the CSI setting based on distribution, provider, and
// whether a KSail-managed CSI component is installed.
func (d *ComponentDetector) detectCSI(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
) (v1alpha1.CSI, error) {
	// K3s bundles local-path-provisioner in kube-system. When the user disables
	// CSI (--csi Disabled), K3s is started with --disable=local-storage and the
	// deployment won't exist. Probe the cluster to distinguish Default from Disabled.
	if distribution == v1alpha1.DistributionK3s {
		if d.deploymentExists(ctx, DeploymentLocalPathProvisionerK3s, NamespaceKubeSystem) {
			return v1alpha1.CSIDefault, nil
		}

		return v1alpha1.CSIDisabled, nil
	}

	// Talos+Hetzner: check for hcloud-csi
	if distribution == v1alpha1.DistributionTalos && provider == v1alpha1.ProviderHetzner {
		exists, err := d.helmClient.ReleaseExists(ctx, ReleaseHCloudCSI, NamespaceHCloudCSI)
		if err != nil {
			return v1alpha1.CSIDefault, fmt.Errorf("check hcloud-csi release: %w", err)
		}

		if exists {
			return v1alpha1.CSIEnabled, nil
		}

		return v1alpha1.CSIDisabled, nil
	}

	// Vanilla/Talos-Docker: check for local-path-provisioner Deployment
	if d.deploymentExists(ctx, DeploymentLocalPathProvisioner, NamespaceLocalPathStorage) {
		return v1alpha1.CSIEnabled, nil
	}

	return v1alpha1.CSIDefault, nil
}

// detectMetricsServer checks for a KSail-managed metrics-server Helm release.
// For K3s, it also probes the built-in metrics-server deployment to detect
// whether the user disabled it via --disable=metrics-server.
func (d *ComponentDetector) detectMetricsServer(
	ctx context.Context,
	distribution v1alpha1.Distribution,
) (v1alpha1.MetricsServer, error) {
	exists, err := d.helmClient.ReleaseExists(
		ctx, ReleaseMetricsServer, NamespaceMetricsServer,
	)
	if err != nil {
		return v1alpha1.MetricsServerDefault, fmt.Errorf("check metrics-server release: %w", err)
	}

	if exists {
		return v1alpha1.MetricsServerEnabled, nil
	}

	// K3s bundles metrics-server in kube-system. When the user disables it
	// (--metrics-server Disabled), K3s is started with --disable=metrics-server
	// and the deployment won't exist. Probe the cluster to distinguish
	// Default (built-in running) from Disabled (explicitly turned off).
	if distribution.ProvidesMetricsServerByDefault() {
		if d.deploymentExists(ctx, DeploymentMetricsServerK3s, NamespaceKubeSystem) {
			return v1alpha1.MetricsServerDefault, nil
		}

		return v1alpha1.MetricsServerDisabled, nil
	}

	return v1alpha1.MetricsServerDefault, nil
}

// detectLoadBalancer determines whether cloud-provider-kind or K3s ServiceLB
// is active.
func (d *ComponentDetector) detectLoadBalancer(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	_ v1alpha1.Provider,
) (v1alpha1.LoadBalancer, error) {
	// K3s bundles ServiceLB. When the user disables it (--load-balancer Disabled),
	// K3s is started with --disable=servicelb and no svclb DaemonSets are created.
	// Probe the cluster for svclb DaemonSets to distinguish Default from Disabled.
	if distribution == v1alpha1.DistributionK3s {
		if d.daemonSetExistsWithLabel(ctx, LabelServiceLBK3s) {
			return v1alpha1.LoadBalancerDefault, nil
		}

		// K3s Traefik (installed by default) creates a LoadBalancer service that
		// triggers svclb DaemonSets. If Traefik is running but no svclb DaemonSets
		// exist, ServiceLB was explicitly disabled.
		if d.deploymentExists(ctx, "traefik", NamespaceKubeSystem) {
			return v1alpha1.LoadBalancerDisabled, nil
		}

		// Traefik is also disabled — no evidence either way.
		// Return Default since we cannot determine the state definitively.
		return v1alpha1.LoadBalancerDefault, nil
	}

	// Vanilla: check for Docker container
	if distribution == v1alpha1.DistributionVanilla && d.dockerClient != nil {
		found, err := d.containerExists(ctx, ContainerCloudProviderKind)
		if err != nil {
			return v1alpha1.LoadBalancerDefault, fmt.Errorf(
				"check cloud-provider-kind container: %w", err,
			)
		}

		if found {
			return v1alpha1.LoadBalancerEnabled, nil
		}
	}

	return v1alpha1.LoadBalancerDefault, nil
}

// detectCertManager checks for a cert-manager Helm release.
func (d *ComponentDetector) detectCertManager(ctx context.Context) (v1alpha1.CertManager, error) {
	exists, err := d.helmClient.ReleaseExists(ctx, ReleaseCertManager, NamespaceCertManager)
	if err != nil {
		return v1alpha1.CertManagerDisabled, fmt.Errorf("check cert-manager release: %w", err)
	}

	if exists {
		return v1alpha1.CertManagerEnabled, nil
	}

	return v1alpha1.CertManagerDisabled, nil
}

// detectPolicyEngine checks for Kyverno or Gatekeeper Helm releases.
func (d *ComponentDetector) detectPolicyEngine(
	ctx context.Context,
) (v1alpha1.PolicyEngine, error) {
	kyverno, err := d.helmClient.ReleaseExists(ctx, ReleaseKyverno, NamespaceKyverno)
	if err != nil {
		return v1alpha1.PolicyEngineNone, fmt.Errorf("check kyverno release: %w", err)
	}

	if kyverno {
		return v1alpha1.PolicyEngineKyverno, nil
	}

	gatekeeper, err := d.helmClient.ReleaseExists(ctx, ReleaseGatekeeper, NamespaceGatekeeper)
	if err != nil {
		return v1alpha1.PolicyEngineNone, fmt.Errorf("check gatekeeper release: %w", err)
	}

	if gatekeeper {
		return v1alpha1.PolicyEngineGatekeeper, nil
	}

	return v1alpha1.PolicyEngineNone, nil
}

// detectGitOpsEngine checks for Flux or ArgoCD Helm releases.
func (d *ComponentDetector) detectGitOpsEngine(
	ctx context.Context,
) (v1alpha1.GitOpsEngine, error) {
	flux, err := d.helmClient.ReleaseExists(ctx, ReleaseFluxOperator, NamespaceFluxOperator)
	if err != nil {
		return v1alpha1.GitOpsEngineNone, fmt.Errorf("check flux-operator release: %w", err)
	}

	if flux {
		return v1alpha1.GitOpsEngineFlux, nil
	}

	argocd, err := d.helmClient.ReleaseExists(ctx, ReleaseArgoCD, NamespaceArgoCD)
	if err != nil {
		return v1alpha1.GitOpsEngineNone, fmt.Errorf("check argocd release: %w", err)
	}

	if argocd {
		return v1alpha1.GitOpsEngineArgoCD, nil
	}

	return v1alpha1.GitOpsEngineNone, nil
}

// deploymentExists checks whether a Deployment with the given name exists in
// the specified namespace.
func (d *ComponentDetector) deploymentExists(
	ctx context.Context,
	name, namespace string,
) bool {
	if d.k8sClientset == nil {
		return false
	}

	_, err := d.k8sClientset.AppsV1().Deployments(namespace).Get(
		ctx, name, metav1.GetOptions{},
	)
	if err != nil {
		// Any error (including not-found) means the deployment is not available.
		return false
	}

	return true
}

// daemonSetExistsWithLabel checks whether any DaemonSet with the given label
// key exists across all namespaces. This is used to detect K3s ServiceLB, which
// creates DaemonSets labeled with svccontroller.k3s.cattle.io/svcname.
func (d *ComponentDetector) daemonSetExistsWithLabel(
	ctx context.Context,
	labelKey string,
) bool {
	if d.k8sClientset == nil {
		return false
	}

	daemonSets, err := d.k8sClientset.AppsV1().DaemonSets("").List(
		ctx, metav1.ListOptions{
			LabelSelector: labelKey,
			Limit:         1,
		},
	)
	if err != nil {
		return false
	}

	return len(daemonSets.Items) > 0
}

// containerExists checks whether a Docker container with the given name is
// running.
func (d *ComponentDetector) containerExists(
	ctx context.Context,
	containerName string,
) (bool, error) {
	if d.dockerClient == nil {
		return false, nil
	}

	containers, err := d.dockerClient.ContainerList(ctx,
		container.ListOptions{
			Filters: filters.NewArgs(
				filters.Arg("name", "^/"+containerName+"$"),
			),
		},
	)
	if err != nil {
		return false, fmt.Errorf("list containers: %w", err)
	}

	return len(containers) > 0, nil
}
