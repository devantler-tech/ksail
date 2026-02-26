package detector

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewComponentDetector(t *testing.T) {
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()
	dockerClient := docker.NewMockAPIClient(t)

	detector := NewComponentDetector(helmClient, k8sClientset, dockerClient)

	assert.NotNil(t, detector)
	assert.Equal(t, helmClient, detector.helmClient)
	assert.Equal(t, k8sClientset, detector.k8sClientset)
	assert.Equal(t, dockerClient, detector.dockerClient)
}

func TestNewComponentDetector_NilDockerClient(t *testing.T) {
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	detector := NewComponentDetector(helmClient, k8sClientset, nil)

	assert.NotNil(t, detector)
	assert.Nil(t, detector.dockerClient)
}

func TestDetectCNI_Cilium(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseCilium, NamespaceCilium).Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	cni, err := detector.detectCNI(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CNICilium, cni)
	helmClient.AssertExpectations(t)
}

func TestDetectCNI_Calico(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseCilium, NamespaceCilium).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseCalico, NamespaceCalico).Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	cni, err := detector.detectCNI(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CNICalico, cni)
	helmClient.AssertExpectations(t)
}

func TestDetectCNI_Default(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseCilium, NamespaceCilium).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseCalico, NamespaceCalico).Return(false, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	cni, err := detector.detectCNI(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CNIDefault, cni)
	helmClient.AssertExpectations(t)
}

func TestDetectCNI_Error(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseCilium, NamespaceCilium).
		Return(false, errors.New("helm error"))

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := detector.detectCNI(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "helm error")
	helmClient.AssertExpectations(t)
}

func TestDetectCSI_K3s_Enabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	// Create a fake deployment for local-path-provisioner
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentLocalPathProvisionerK3s,
			Namespace: NamespaceKubeSystem,
		},
	}
	k8sClientset := fake.NewSimpleClientset(deployment)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := detector.detectCSI(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDefault, csi)
}

func TestDetectCSI_K3s_Disabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset() // No deployments

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := detector.detectCSI(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDisabled, csi)
}

func TestDetectCSI_TalosHetzner_Enabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseHCloudCSI, NamespaceHCloudCSI).Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := detector.detectCSI(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIEnabled, csi)
	helmClient.AssertExpectations(t)
}

func TestDetectCSI_TalosHetzner_Disabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseHCloudCSI, NamespaceHCloudCSI).Return(false, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := detector.detectCSI(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDisabled, csi)
	helmClient.AssertExpectations(t)
}

func TestDetectCSI_Vanilla_Enabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	// Create a fake deployment for local-path-provisioner
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentLocalPathProvisioner,
			Namespace: NamespaceLocalPathStorage,
		},
	}
	k8sClientset := fake.NewSimpleClientset(deployment)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := detector.detectCSI(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIEnabled, csi)
}

func TestDetectCSI_Vanilla_Default(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := detector.detectCSI(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDefault, csi)
}

func TestDetectMetricsServer_Enabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseMetricsServer, NamespaceMetricsServer).
		Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	metricsServer, err := detector.detectMetricsServer(ctx, v1alpha1.DistributionVanilla)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.MetricsServerEnabled, metricsServer)
	helmClient.AssertExpectations(t)
}

func TestDetectMetricsServer_K3s_Default(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	// Create a fake deployment for K3s metrics-server
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentMetricsServerK3s,
			Namespace: NamespaceKubeSystem,
		},
	}
	k8sClientset := fake.NewSimpleClientset(deployment)

	helmClient.On("ReleaseExists", ctx, ReleaseMetricsServer, NamespaceMetricsServer).
		Return(false, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	metricsServer, err := detector.detectMetricsServer(ctx, v1alpha1.DistributionK3s)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.MetricsServerDefault, metricsServer)
	helmClient.AssertExpectations(t)
}

func TestDetectMetricsServer_K3s_Disabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset() // No deployments

	helmClient.On("ReleaseExists", ctx, ReleaseMetricsServer, NamespaceMetricsServer).
		Return(false, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	metricsServer, err := detector.detectMetricsServer(ctx, v1alpha1.DistributionK3s)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.MetricsServerDisabled, metricsServer)
	helmClient.AssertExpectations(t)
}

func TestDetectLoadBalancer_K3s_Enabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	// Create a fake DaemonSet with ServiceLB label
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svclb-traefik",
			Namespace: NamespaceKubeSystem,
			Labels: map[string]string{
				LabelServiceLBK3s: "traefik",
			},
		},
	}
	k8sClientset := fake.NewSimpleClientset(daemonSet)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	lb, err := detector.detectLoadBalancer(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, lb)
}

func TestDetectLoadBalancer_K3s_Disabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	// Create Traefik deployment but no svclb DaemonSets
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "traefik",
			Namespace: NamespaceKubeSystem,
		},
	}
	k8sClientset := fake.NewSimpleClientset(deployment)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	lb, err := detector.detectLoadBalancer(ctx, v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDisabled, lb)
}

func TestDetectLoadBalancer_Vanilla_Enabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()
	dockerClient := docker.NewMockAPIClient(t)

	// Mock container list to return cloud-provider-kind container
	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return([]container.Summary{{Names: []string{"/ksail-cloud-provider-kind"}}}, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, dockerClient)
	lb, err := detector.detectLoadBalancer(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerEnabled, lb)
	dockerClient.AssertExpectations(t)
}

func TestDetectLoadBalancer_Vanilla_Disabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()
	dockerClient := docker.NewMockAPIClient(t)

	// Mock container list to return no containers
	dockerClient.On("ContainerList", ctx, mock.Anything).Return([]container.Summary{}, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, dockerClient)
	lb, err := detector.detectLoadBalancer(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, lb)
	dockerClient.AssertExpectations(t)
}

func TestDetectLoadBalancer_Talos_MetalLB_Enabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseMetalLB, NamespaceMetalLB).Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	lb, err := detector.detectLoadBalancer(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerEnabled, lb)
	helmClient.AssertExpectations(t)
}

func TestDetectLoadBalancer_Talos_MetalLB_Default(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseMetalLB, NamespaceMetalLB).Return(false, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	lb, err := detector.detectLoadBalancer(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, lb)
	helmClient.AssertExpectations(t)
}

func TestDetectCertManager_Enabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseCertManager, NamespaceCertManager).Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	certManager, err := detector.detectCertManager(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CertManagerEnabled, certManager)
	helmClient.AssertExpectations(t)
}

func TestDetectCertManager_Disabled(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseCertManager, NamespaceCertManager).Return(false, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	certManager, err := detector.detectCertManager(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CertManagerDisabled, certManager)
	helmClient.AssertExpectations(t)
}

func TestDetectPolicyEngine_Kyverno(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseKyverno, NamespaceKyverno).Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	policyEngine, err := detector.detectPolicyEngine(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.PolicyEngineKyverno, policyEngine)
	helmClient.AssertExpectations(t)
}

func TestDetectPolicyEngine_Gatekeeper(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseKyverno, NamespaceKyverno).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseGatekeeper, NamespaceGatekeeper).Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	policyEngine, err := detector.detectPolicyEngine(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.PolicyEngineGatekeeper, policyEngine)
	helmClient.AssertExpectations(t)
}

func TestDetectPolicyEngine_None(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseKyverno, NamespaceKyverno).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseGatekeeper, NamespaceGatekeeper).Return(false, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	policyEngine, err := detector.detectPolicyEngine(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.PolicyEngineNone, policyEngine)
	helmClient.AssertExpectations(t)
}

func TestDetectGitOpsEngine_Flux(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseFluxOperator, NamespaceFluxOperator).Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	gitOpsEngine, err := detector.detectGitOpsEngine(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.GitOpsEngineFlux, gitOpsEngine)
	helmClient.AssertExpectations(t)
}

func TestDetectGitOpsEngine_ArgoCD(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseFluxOperator, NamespaceFluxOperator).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseArgoCD, NamespaceArgoCD).Return(true, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	gitOpsEngine, err := detector.detectGitOpsEngine(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.GitOpsEngineArgoCD, gitOpsEngine)
	helmClient.AssertExpectations(t)
}

func TestDetectGitOpsEngine_None(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseFluxOperator, NamespaceFluxOperator).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseArgoCD, NamespaceArgoCD).Return(false, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	gitOpsEngine, err := detector.detectGitOpsEngine(ctx)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.GitOpsEngineNone, gitOpsEngine)
	helmClient.AssertExpectations(t)
}

func TestDetectComponents_Success(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	// Mock all the Helm release checks
	helmClient.On("ReleaseExists", ctx, ReleaseCilium, NamespaceCilium).Return(true, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseMetricsServer, NamespaceMetricsServer).Return(true, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseCertManager, NamespaceCertManager).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseKyverno, NamespaceKyverno).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseGatekeeper, NamespaceGatekeeper).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseFluxOperator, NamespaceFluxOperator).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseArgoCD, NamespaceArgoCD).Return(false, nil)
	helmClient.On("ReleaseExists", ctx, ReleaseMetalLB, NamespaceMetalLB).Return(false, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	spec, err := detector.DetectComponents(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)

	assert.NoError(t, err)
	assert.NotNil(t, spec)
	assert.Equal(t, v1alpha1.DistributionTalos, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, spec.Provider)
	assert.Equal(t, v1alpha1.CNICilium, spec.CNI)
	assert.Equal(t, v1alpha1.MetricsServerEnabled, spec.MetricsServer)
	helmClient.AssertExpectations(t)
}

func TestDetectComponents_ErrorOnCNI(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	helmClient.On("ReleaseExists", ctx, ReleaseCilium, NamespaceCilium).
		Return(false, errors.New("helm error"))

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := detector.DetectComponents(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "detect CNI")
	helmClient.AssertExpectations(t)
}

func TestDeploymentExists_Found(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-namespace",
		},
	}
	k8sClientset := fake.NewSimpleClientset(deployment)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	exists := detector.deploymentExists(ctx, "test-deployment", "test-namespace")

	assert.True(t, exists)
}

func TestDeploymentExists_NotFound(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	exists := detector.deploymentExists(ctx, "nonexistent", "test-namespace")

	assert.False(t, exists)
}

func TestDeploymentExists_NilClientset(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	detector := NewComponentDetector(helmClient, nil, nil)
	exists := detector.deploymentExists(ctx, "test", "test")

	assert.False(t, exists)
}

func TestDaemonSetExistsWithLabel_Found(t *testing.T) {
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
	k8sClientset := fake.NewSimpleClientset(daemonSet)

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	exists := detector.daemonSetExistsWithLabel(ctx, "test-label")

	assert.True(t, exists)
}

func TestDaemonSetExistsWithLabel_NotFound(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	exists := detector.daemonSetExistsWithLabel(ctx, "nonexistent-label")

	assert.False(t, exists)
}

func TestDaemonSetExistsWithLabel_NilClientset(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	detector := NewComponentDetector(helmClient, nil, nil)
	exists := detector.daemonSetExistsWithLabel(ctx, "test-label")

	assert.False(t, exists)
}

func TestContainerExists_Found(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()
	dockerClient := docker.NewMockAPIClient(t)

	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return([]container.Summary{{Names: []string{"/test-container"}}}, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, dockerClient)
	exists, err := detector.containerExists(ctx, "test-container")

	assert.NoError(t, err)
	assert.True(t, exists)
	dockerClient.AssertExpectations(t)
}

func TestContainerExists_NotFound(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()
	dockerClient := docker.NewMockAPIClient(t)

	dockerClient.On("ContainerList", ctx, mock.Anything).Return([]container.Summary{}, nil)

	detector := NewComponentDetector(helmClient, k8sClientset, dockerClient)
	exists, err := detector.containerExists(ctx, "nonexistent")

	assert.NoError(t, err)
	assert.False(t, exists)
	dockerClient.AssertExpectations(t)
}

func TestContainerExists_NilDockerClient(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()

	detector := NewComponentDetector(helmClient, k8sClientset, nil)
	exists, err := detector.containerExists(ctx, "test")

	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestContainerExists_Error(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewSimpleClientset()
	dockerClient := docker.NewMockAPIClient(t)

	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return(nil, errors.New("docker error"))

	detector := NewComponentDetector(helmClient, k8sClientset, dockerClient)
	_, err := detector.containerExists(ctx, "test")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "docker error")
	dockerClient.AssertExpectations(t)
}

func TestDetectFirstRelease_FirstFound(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	helmClient.On("ReleaseExists", ctx, "release1", "namespace1").Return(true, nil)

	mappings := []releaseMapping[v1alpha1.CNI]{
		{release: "release1", namespace: "namespace1", value: v1alpha1.CNICilium},
		{release: "release2", namespace: "namespace2", value: v1alpha1.CNICalico},
	}

	result, err := detectFirstRelease(ctx, helmClient, mappings, v1alpha1.CNIDefault)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CNICilium, result)
	helmClient.AssertExpectations(t)
}

func TestDetectFirstRelease_SecondFound(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	helmClient.On("ReleaseExists", ctx, "release1", "namespace1").Return(false, nil)
	helmClient.On("ReleaseExists", ctx, "release2", "namespace2").Return(true, nil)

	mappings := []releaseMapping[v1alpha1.CNI]{
		{release: "release1", namespace: "namespace1", value: v1alpha1.CNICilium},
		{release: "release2", namespace: "namespace2", value: v1alpha1.CNICalico},
	}

	result, err := detectFirstRelease(ctx, helmClient, mappings, v1alpha1.CNIDefault)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CNICalico, result)
	helmClient.AssertExpectations(t)
}

func TestDetectFirstRelease_NoneFound(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	helmClient.On("ReleaseExists", ctx, "release1", "namespace1").Return(false, nil)
	helmClient.On("ReleaseExists", ctx, "release2", "namespace2").Return(false, nil)

	mappings := []releaseMapping[v1alpha1.CNI]{
		{release: "release1", namespace: "namespace1", value: v1alpha1.CNICilium},
		{release: "release2", namespace: "namespace2", value: v1alpha1.CNICalico},
	}

	result, err := detectFirstRelease(ctx, helmClient, mappings, v1alpha1.CNIDefault)

	assert.NoError(t, err)
	assert.Equal(t, v1alpha1.CNIDefault, result)
	helmClient.AssertExpectations(t)
}

func TestDetectFirstRelease_Error(t *testing.T) {
	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)

	helmClient.On("ReleaseExists", ctx, "release1", "namespace1").
		Return(false, errors.New("helm error"))

	mappings := []releaseMapping[v1alpha1.CNI]{
		{release: "release1", namespace: "namespace1", value: v1alpha1.CNICilium},
	}

	_, err := detectFirstRelease(ctx, helmClient, mappings, v1alpha1.CNIDefault)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "helm error")
	helmClient.AssertExpectations(t)
}
