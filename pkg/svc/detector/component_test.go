package detector_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/detector"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	errHelm   = errors.New("helm error")
	errDocker = errors.New("docker error")
)

func TestNewComponentDetector(t *testing.T) {
	t.Parallel()

	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)

	assert.NotNil(t, d)
}

func TestNewComponentDetector_NilDockerClient(t *testing.T) {
	t.Parallel()

	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)

	assert.NotNil(t, d)
}

func TestDetectCNI_Cilium(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cni, err := d.ExportDetectCNI(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CNICilium, cni)
}

func TestDetectCNI_Calico(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCalico, detector.NamespaceCalico).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cni, err := d.ExportDetectCNI(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CNICalico, cni)
}

func TestDetectCNI_Default(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCalico, detector.NamespaceCalico).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cni, err := d.ExportDetectCNI(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CNIDefault, cni)
}

func TestDetectCNI_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, errHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.ExportDetectCNI(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm error")
}

func TestDetectCSI_K3s_Enabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      detector.DeploymentLocalPathProvisionerK3s,
			Namespace: detector.NamespaceKubeSystem,
		},
	}
	k8sClientset := fake.NewClientset(deployment)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDefault, csi)
}

func TestDetectCSI_K3s_Disabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDisabled, csi)
}

func TestDetectCSI_TalosHetzner_Enabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseHCloudCSI, detector.NamespaceHCloudCSI).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIEnabled, csi)
}

func TestDetectCSI_TalosHetzner_Disabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseHCloudCSI, detector.NamespaceHCloudCSI).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDisabled, csi)
}

func TestDetectCSI_Vanilla_Enabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      detector.DeploymentLocalPathProvisioner,
			Namespace: detector.NamespaceLocalPathStorage,
		},
	}
	k8sClientset := fake.NewClientset(deployment)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIEnabled, csi)
}

func TestDetectCSI_Vanilla_Default(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDefault, csi)
}

func TestDetectMetricsServer_Enabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	metricsServer, err := d.ExportDetectMetricsServer(ctx, v1alpha1.DistributionVanilla)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.MetricsServerEnabled, metricsServer)
}

func TestDetectMetricsServer_K3s_Default(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      detector.DeploymentMetricsServerK3s,
			Namespace: detector.NamespaceKubeSystem,
		},
	}
	k8sClientset := fake.NewClientset(deployment)

	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	metricsServer, err := d.ExportDetectMetricsServer(ctx, v1alpha1.DistributionK3s)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.MetricsServerDefault, metricsServer)
}

func TestDetectMetricsServer_K3s_Disabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	metricsServer, err := d.ExportDetectMetricsServer(ctx, v1alpha1.DistributionK3s)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.MetricsServerDisabled, metricsServer)
}

func TestDetectLoadBalancer_K3s_Enabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svclb-traefik",
			Namespace: detector.NamespaceKubeSystem,
			Labels: map[string]string{
				detector.LabelServiceLBK3s: "traefik",
			},
		},
	}
	k8sClientset := fake.NewClientset(daemonSet)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionK3s,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, loadBalancer)
}

func TestDetectLoadBalancer_K3s_Disabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "traefik",
			Namespace: detector.NamespaceKubeSystem,
		},
	}
	k8sClientset := fake.NewClientset(deployment)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionK3s,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDisabled, loadBalancer)
}

func TestDetectLoadBalancer_Vanilla_Enabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return([]container.Summary{{Names: []string{"/ksail-cloud-provider-kind"}}}, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerEnabled, loadBalancer)
}

func TestDetectLoadBalancer_Vanilla_Disabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	dockerClient.On("ContainerList", ctx, mock.Anything).Return([]container.Summary{}, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, loadBalancer)
}

func TestDetectLoadBalancer_Talos_MetalLB_Enabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetalLB, detector.NamespaceMetalLB).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerEnabled, loadBalancer)
}

func TestDetectLoadBalancer_Talos_MetalLB_Default(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetalLB, detector.NamespaceMetalLB).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, loadBalancer)
}

func TestDetectCertManager_Enabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseCertManager, detector.NamespaceCertManager).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	certManager, err := d.ExportDetectCertManager(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CertManagerEnabled, certManager)
}

func TestDetectCertManager_Disabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseCertManager, detector.NamespaceCertManager).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	certManager, err := d.ExportDetectCertManager(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CertManagerDisabled, certManager)
}

func TestDetectPolicyEngine_Kyverno(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseKyverno, detector.NamespaceKyverno).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	policyEngine, err := d.ExportDetectPolicyEngine(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.PolicyEngineKyverno, policyEngine)
}

func TestDetectPolicyEngine_Gatekeeper(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseKyverno, detector.NamespaceKyverno).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseGatekeeper, detector.NamespaceGatekeeper).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	policyEngine, err := d.ExportDetectPolicyEngine(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.PolicyEngineGatekeeper, policyEngine)
}

func TestDetectPolicyEngine_None(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseKyverno, detector.NamespaceKyverno).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseGatekeeper, detector.NamespaceGatekeeper).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	policyEngine, err := d.ExportDetectPolicyEngine(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.PolicyEngineNone, policyEngine)
}

func TestDetectGitOpsEngine_Flux(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseFluxOperator, detector.NamespaceFluxOperator).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	gitOpsEngine, err := d.ExportDetectGitOpsEngine(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.GitOpsEngineFlux, gitOpsEngine)
}

func TestDetectGitOpsEngine_ArgoCD(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseFluxOperator, detector.NamespaceFluxOperator).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseArgoCD, detector.NamespaceArgoCD).
		Return(true, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	gitOpsEngine, err := d.ExportDetectGitOpsEngine(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.GitOpsEngineArgoCD, gitOpsEngine)
}

func TestDetectGitOpsEngine_None(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseFluxOperator, detector.NamespaceFluxOperator).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseArgoCD, detector.NamespaceArgoCD).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	gitOpsEngine, err := d.ExportDetectGitOpsEngine(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.GitOpsEngineNone, gitOpsEngine)
}

func TestDetectComponents_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(true, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetricsServer, detector.NamespaceMetricsServer).
		Return(true, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseCertManager, detector.NamespaceCertManager).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseKyverno, detector.NamespaceKyverno).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseGatekeeper, detector.NamespaceGatekeeper).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseFluxOperator, detector.NamespaceFluxOperator).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseArgoCD, detector.NamespaceArgoCD).
		Return(false, nil)
	helmClient.On("ReleaseExists", ctx, detector.ReleaseMetalLB, detector.NamespaceMetalLB).
		Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	spec, err := d.DetectComponents(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.NotNil(t, spec)
	assert.Equal(t, v1alpha1.DistributionTalos, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, spec.Provider)
	assert.Equal(t, v1alpha1.CNICilium, spec.CNI)
	assert.Equal(t, v1alpha1.MetricsServerEnabled, spec.MetricsServer)
}

func TestDetectComponents_ErrorOnCNI(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx, detector.ReleaseCilium, detector.NamespaceCilium).
		Return(false, errHelm)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect CNI")
}

func TestDeploymentExists_Found(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-namespace",
		},
	}
	k8sClientset := fake.NewClientset(deployment)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	exists := d.ExportDeploymentExists(ctx, "test-deployment", "test-namespace")

	assert.True(t, exists)
}

func TestDeploymentExists_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	exists := d.ExportDeploymentExists(ctx, "nonexistent", "test-namespace")

	assert.False(t, exists)
}

func TestDeploymentExists_NilClientset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	d := detector.NewComponentDetector(helmClient, nil, nil)
	exists := d.ExportDeploymentExists(ctx, "test", "test")

	assert.False(t, exists)
}

func TestDaemonSetExistsWithLabel_Found(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-daemonset",
			Namespace: "test-namespace",
			Labels: map[string]string{
				"test-label": "test-value",
			},
		},
	}
	k8sClientset := fake.NewClientset(daemonSet)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	exists := d.ExportDaemonSetExistsWithLabel(ctx, "test-label")

	assert.True(t, exists)
}

func TestDaemonSetExistsWithLabel_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	exists := d.ExportDaemonSetExistsWithLabel(ctx, "nonexistent-label")

	assert.False(t, exists)
}

func TestDaemonSetExistsWithLabel_NilClientset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	d := detector.NewComponentDetector(helmClient, nil, nil)
	exists := d.ExportDaemonSetExistsWithLabel(ctx, "test-label")

	assert.False(t, exists)
}

func TestContainerExists_Found(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return([]container.Summary{{Names: []string{"/test-container"}}}, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
	exists, err := d.ExportContainerExists(ctx, "test-container")

	require.NoError(t, err)
	assert.True(t, exists)
}

func TestContainerExists_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	dockerClient.On("ContainerList", ctx, mock.Anything).Return([]container.Summary{}, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
	exists, err := d.ExportContainerExists(ctx, "nonexistent")

	require.NoError(t, err)
	assert.False(t, exists)
}

func TestContainerExists_NilDockerClient(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	exists, err := d.ExportContainerExists(ctx, "test")

	require.NoError(t, err)
	assert.False(t, exists)
}

func TestContainerExists_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return(nil, errDocker)

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
	_, err := d.ExportContainerExists(ctx, "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker error")
}

func TestDetectFirstRelease_FirstFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	helmClient.On("ReleaseExists", ctx, "release1", "namespace1").Return(true, nil)

	mappings := []detector.ReleaseMappingForTest[v1alpha1.CNI]{
		detector.NewReleaseMappingForTest[v1alpha1.CNI](
			"release1",
			"namespace1",
			v1alpha1.CNICilium,
		),
		detector.NewReleaseMappingForTest[v1alpha1.CNI](
			"release2",
			"namespace2",
			v1alpha1.CNICalico,
		),
	}

	result, err := detector.ExportDetectFirstRelease(ctx, helmClient, mappings, v1alpha1.CNIDefault)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CNICilium, result)
}

func TestDetectFirstRelease_SecondFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	helmClient.On("ReleaseExists", ctx, "release1", "namespace1").Return(false, nil)
	helmClient.On("ReleaseExists", ctx, "release2", "namespace2").Return(true, nil)

	mappings := []detector.ReleaseMappingForTest[v1alpha1.CNI]{
		detector.NewReleaseMappingForTest[v1alpha1.CNI](
			"release1",
			"namespace1",
			v1alpha1.CNICilium,
		),
		detector.NewReleaseMappingForTest[v1alpha1.CNI](
			"release2",
			"namespace2",
			v1alpha1.CNICalico,
		),
	}

	result, err := detector.ExportDetectFirstRelease(ctx, helmClient, mappings, v1alpha1.CNIDefault)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CNICalico, result)
}

func TestDetectFirstRelease_NoneFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	helmClient.On("ReleaseExists", ctx, "release1", "namespace1").Return(false, nil)
	helmClient.On("ReleaseExists", ctx, "release2", "namespace2").Return(false, nil)

	mappings := []detector.ReleaseMappingForTest[v1alpha1.CNI]{
		detector.NewReleaseMappingForTest[v1alpha1.CNI](
			"release1",
			"namespace1",
			v1alpha1.CNICilium,
		),
		detector.NewReleaseMappingForTest[v1alpha1.CNI](
			"release2",
			"namespace2",
			v1alpha1.CNICalico,
		),
	}

	result, err := detector.ExportDetectFirstRelease(ctx, helmClient, mappings, v1alpha1.CNIDefault)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CNIDefault, result)
}

func TestDetectFirstRelease_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	helmClient.On("ReleaseExists", ctx, "release1", "namespace1").
		Return(false, errHelm)

	mappings := []detector.ReleaseMappingForTest[v1alpha1.CNI]{
		detector.NewReleaseMappingForTest[v1alpha1.CNI](
			"release1",
			"namespace1",
			v1alpha1.CNICilium,
		),
	}

	_, err := detector.ExportDetectFirstRelease(ctx, helmClient, mappings, v1alpha1.CNIDefault)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm error")
}
