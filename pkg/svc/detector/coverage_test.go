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

// TestDetectComponents_AllReleasesFromCache verifies that DetectComponents
// correctly detects multiple installed components via a single ListReleases call,
// confirming the in-memory cache serves all subsequent lookups.
func TestDetectComponents_AllReleasesFromCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// All installed releases are returned in a single ListReleases call.
	helmClient.On("ListReleases", ctx).Return([]helm.ReleaseInfo{
		{Name: detector.ReleaseCilium, Namespace: detector.NamespaceCilium},
		{Name: detector.ReleaseMetricsServer, Namespace: detector.NamespaceMetricsServer},
		{Name: detector.ReleaseCertManager, Namespace: detector.NamespaceCertManager},
		{Name: detector.ReleaseKyverno, Namespace: detector.NamespaceKyverno},
		{Name: detector.ReleaseFluxOperator, Namespace: detector.NamespaceFluxOperator},
	}, nil).Once()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	spec, err := d.DetectComponents(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CNICilium, spec.CNI)
	assert.Equal(t, v1alpha1.MetricsServerEnabled, spec.MetricsServer)
	assert.Equal(t, v1alpha1.CertManagerEnabled, spec.CertManager)
	assert.Equal(t, v1alpha1.PolicyEngineKyverno, spec.PolicyEngine)
	assert.Equal(t, v1alpha1.GitOpsEngineFlux, spec.GitOpsEngine)
}

// TestDetectComponents_LoadBalancerError verifies DetectComponents wraps load
// balancer detection errors properly. The Docker client error occurs after the
// single ListReleases call, validating non-Helm error propagation.
func TestDetectComponents_LoadBalancerError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	// ListReleases is called once; no releases installed.
	helmClient.On("ListReleases", ctx).Return([]helm.ReleaseInfo{}, nil).Once()
	// LoadBalancer: Vanilla with Docker client that returns error.
	dockerClient.On("ContainerList", ctx, mock.AnythingOfType("container.ListOptions")).
		Return([]container.Summary{}, errDetectorDocker)

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect LoadBalancer")
}

// TestDetectComponents_FallbackPropagatesError verifies that when ListReleases fails
// with a non-context error (RBAC fallback), DetectComponents wraps downstream
// ReleaseExists errors with the correct "detect <Component>" context string.
func TestDetectComponents_FallbackPropagatesError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// ListReleases fails (RBAC restriction) → fall back to per-release checks.
	helmClient.On("ListReleases", ctx).Return(nil, errDetectorHelm).Once()
	// The first per-release check is detectCNI → Cilium. Return an error so the
	// failure surfaces as a wrapped "detect CNI" error.
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, errDetectorHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect CNI")
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
