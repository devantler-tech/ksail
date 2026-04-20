package detector_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	errDetectorHelm   = errors.New("helm error")
	errDetectorDocker = errors.New("docker error")
)

// TestDetectComponents_CSIError verifies that DetectComponents returns an
// error when CSI detection fails.
func TestDetectComponents_CSIError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// CNI succeeds (no Cilium or Calico)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCalico, detector.NamespaceCalico).
		Return(false, nil)
	// CSI: Talos+Hetzner triggers ReleaseExists for hcloud-csi which errors
	helmClient.On("ReleaseExists", ctx, detector.ReleaseHCloudCSI, detector.NamespaceHCloudCSI).
		Return(false, errDetectorHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect CSI")
}

// TestDetectComponents_MetricsServerError verifies that DetectComponents returns
// an error when MetricsServer detection fails.
func TestDetectComponents_MetricsServerError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// CNI succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCalico, detector.NamespaceCalico).
		Return(false, nil)
	// CSI succeeds (Vanilla distribution, no local-path-provisioner found)
	// MetricsServer errors
	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(false, errDetectorHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect MetricsServer")
}

// TestDetectComponents_LoadBalancerError verifies DetectComponents wraps load
// balancer detection errors properly.
func TestDetectComponents_LoadBalancerError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	// CNI succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCalico, detector.NamespaceCalico).
		Return(false, nil)
	// CSI succeeds (Vanilla, no deployment)
	// MetricsServer succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(false, nil)
	// LoadBalancer: Vanilla with Docker client that returns error
	dockerClient.On("ContainerList", ctx, mock.AnythingOfType("container.ListOptions")).
		Return([]container.Summary{}, errDetectorDocker)

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect LoadBalancer")
}

// TestDetectComponents_CertManagerError verifies DetectComponents wraps
// cert-manager detection errors.
func TestDetectComponents_CertManagerError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// CNI succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCalico, detector.NamespaceCalico).
		Return(false, nil)
	// CSI succeeds (K3s without deployment)
	// MetricsServer succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(false, nil)
	// LoadBalancer succeeds (K3s, no svclb daemonsets, no traefik)
	// CertManager errors
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCertManager, detector.NamespaceCertManager).
		Return(false, errDetectorHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect CertManager")
}

// TestDetectComponents_PolicyEngineError verifies DetectComponents wraps
// policy engine detection errors.
func TestDetectComponents_PolicyEngineError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// CNI succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCalico, detector.NamespaceCalico).
		Return(false, nil)
	// CSI succeeds
	// MetricsServer succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(false, nil)
	// LoadBalancer succeeds
	// CertManager succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCertManager, detector.NamespaceCertManager).
		Return(false, nil)
	// PolicyEngine errors
	helmClient.On("ReleaseExists", ctx, detector.ReleaseKyverno, detector.NamespaceKyverno).
		Return(false, errDetectorHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect PolicyEngine")
}

// TestDetectComponents_GitOpsEngineError verifies DetectComponents wraps
// gitops engine detection errors.
func TestDetectComponents_GitOpsEngineError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// CNI succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCalico, detector.NamespaceCalico).
		Return(false, nil)
	// CSI succeeds
	// MetricsServer succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(false, nil)
	// LoadBalancer succeeds
	// CertManager succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCertManager, detector.NamespaceCertManager).
		Return(false, nil)
	// PolicyEngine succeeds
	helmClient.On("ReleaseExists", ctx, detector.ReleaseKyverno, detector.NamespaceKyverno).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseGatekeeper, detector.NamespaceGatekeeper).
		Return(false, nil)
	// GitOpsEngine errors
	helmClient.On("ReleaseExists", ctx, detector.ReleaseFluxOperator, detector.NamespaceFluxOperator).
		Return(false, errDetectorHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect GitOpsEngine")
}

// TestDetectCSI_VClusterWithCSI verifies CSI detection for VCluster distribution
// with local-path-provisioner present.
func TestDetectCSI_VClusterWithCSI(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      detector.DeploymentLocalPathProvisioner,
				Namespace: detector.NamespaceLocalPathStorage,
			},
		},
	)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionVCluster, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIEnabled, csi)
}

// TestDetectCSI_VClusterWithoutCSI verifies CSI detection for VCluster when
// local-path-provisioner is absent.
func TestDetectCSI_VClusterWithoutCSI(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionVCluster, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDisabled, csi)
}

// TestDetectCSI_TalosHetznerError verifies error handling from hcloud-csi release check.
func TestDetectCSI_TalosHetznerError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseHCloudCSI, detector.NamespaceHCloudCSI).
		Return(false, errDetectorHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hcloud-csi")
}

// TestDetectMetricsServer_ExplicitlyEnabled verifies detection when a
// KSail-managed metrics-server Helm release exists.
func TestDetectMetricsServer_ExplicitlyEnabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	ms, err := d.ExportDetectMetricsServer(ctx, v1alpha1.DistributionVanilla)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.MetricsServerEnabled, ms)
}

// TestDetectMetricsServer_K3sDisabled verifies detection when K3s metrics-server
// deployment is absent (explicitly disabled).
func TestDetectMetricsServer_K3sDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	ms, err := d.ExportDetectMetricsServer(ctx, v1alpha1.DistributionK3s)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.MetricsServerDisabled, ms)
}

// TestDetectMetricsServer_K3sDefault verifies detection when K3s built-in
// metrics-server deployment is present.
func TestDetectMetricsServer_K3sDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      detector.DeploymentMetricsServerK3s,
				Namespace: detector.NamespaceKubeSystem,
			},
		},
	)

	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	ms, err := d.ExportDetectMetricsServer(ctx, v1alpha1.DistributionK3s)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.MetricsServerDefault, ms)
}

// TestDetectLoadBalancer_VanillaNoDocker verifies load balancer detection for
// Vanilla without a Docker client.
func TestDetectLoadBalancer_VanillaNoDocker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, loadBalancer)
}

// TestDetectLoadBalancer_VanillaDockerError verifies error wrapping when
// Docker container listing fails.
func TestDetectLoadBalancer_VanillaDockerError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	dockerClient.On("ContainerList", ctx, mock.AnythingOfType("container.ListOptions")).
		Return([]container.Summary{}, errDetectorDocker)

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
	_, err := d.ExportDetectLoadBalancer(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cloud-provider-kind")
}

// TestDetectMetalLB_Error verifies error handling in MetalLB detection.
func TestDetectMetalLB_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetalLB, detector.NamespaceMetalLB).
		Return(false, errDetectorHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.ExportDetectLoadBalancer(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "metallb")
}

// TestDaemonSetExistsWithLabel_Error verifies that K8s API errors cause
// daemonSetExistsWithLabel to return false rather than propagating the error.
func TestDaemonSetExistsWithLabel_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	// Using nil k8sClientset triggers early return false (nil check)
	d := detector.NewComponentDetector(helmClient, nil, nil)
	result := d.ExportDaemonSetExistsWithLabel(ctx, "nonexistent.label")

	assert.False(t, result)
}
