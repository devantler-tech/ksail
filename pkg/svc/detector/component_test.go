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
	errHelm   = errors.New("helm error")
	errDocker = errors.New("docker error")
	errValues = errors.New("values error")
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

func TestDetectCSI_Vanilla_Default_WithDeployment(t *testing.T) {
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

func TestDetectCSI_Vanilla_Disabled_NoDeployment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	csi, err := d.ExportDetectCSI(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.CSIDisabled, csi)
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

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient).WithRetryDelay(0)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, loadBalancer)
}

func TestDetectLoadBalancer_Vanilla_RetryFindsContainer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	// First call returns empty (container not yet visible), second call finds it.
	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return([]container.Summary{}, nil).Once()
	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return([]container.Summary{{Names: []string{"/ksail-cloud-provider-kind"}}}, nil).Once()

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient).WithRetryDelay(0)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerEnabled, loadBalancer)
}

func TestDetectLoadBalancer_Vanilla_RetryAfterTransientError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	// First call returns a transient error, second call succeeds.
	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return([]container.Summary{}, errDocker).Once()
	dockerClient.On("ContainerList", ctx, mock.Anything).
		Return([]container.Summary{{Names: []string{"/ksail-cloud-provider-kind"}}}, nil).Once()

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient).WithRetryDelay(0)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerEnabled, loadBalancer)
}

func TestDetectLoadBalancer_Vanilla_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()
	dockerClient := docker.NewMockAPIClient(t)

	// First call returns empty, then cancel context before retry.
	// Using .Once() ensures testify fails if a second call is attempted.
	dockerClient.On("ContainerList", mock.Anything, mock.Anything).
		Run(func(_ mock.Arguments) { cancel() }).
		Return([]container.Summary{}, nil).Once()

	d := detector.NewComponentDetector(helmClient, k8sClientset, dockerClient).WithRetryDelay(0)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDefault, loadBalancer)
	dockerClient.AssertNumberOfCalls(t, "ContainerList", 1)
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

func TestDetectLoadBalancer_KWOK_Disabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	loadBalancer, err := d.ExportDetectLoadBalancer(
		ctx,
		v1alpha1.DistributionKWOK,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.LoadBalancerDisabled, loadBalancer)
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

	// DetectComponents calls ListReleases once to build an in-memory release set.
	// The test returns Cilium and metrics-server as installed releases.
	helmClient.On("ListReleases", ctx).Return([]helm.ReleaseInfo{
		{Name: detector.ReleaseCilium, Namespace: detector.NamespaceCilium},
		{Name: detector.ReleaseMetricsServer, Namespace: detector.NamespaceMetricsServer},
	}, nil).Once()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	spec, err := d.DetectComponents(ctx, v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.NotNil(t, spec)
	assert.Equal(t, v1alpha1.DistributionTalos, spec.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, spec.Provider)
	assert.Equal(t, v1alpha1.CNICilium, spec.CNI)
	assert.Equal(t, v1alpha1.MetricsServerEnabled, spec.MetricsServer)
}

func TestDetectComponents_ArgoCD(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// Regression test: ArgoCD release (name "argocd" in namespace "argocd")
	// must be detected correctly via ListReleases → releaseSet lookup.
	helmClient.On("ListReleases", ctx).Return([]helm.ReleaseInfo{
		{Name: detector.ReleaseArgoCD, Namespace: detector.NamespaceArgoCD},
	}, nil).Once()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	spec, err := d.DetectComponents(ctx, v1alpha1.DistributionKWOK, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.NotNil(t, spec)
	assert.Equal(t, v1alpha1.GitOpsEngineArgoCD, spec.GitOpsEngine)
}

func TestDetectComponents_ListReleasesError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately so ctx.Err() is non-nil; error is propagated, not silenced.

	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ListReleases", ctx).Return(nil, errHelm).Once()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	_, err := d.DetectComponents(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list helm releases")
}

func TestDetectComponents_ListReleasesRBACFallback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// ListReleases fails (e.g., restricted RBAC). DetectComponents falls back to
	// per-release ReleaseExists calls on the underlying client.
	helmClient.On("ListReleases", ctx).Return(nil, errHelm).Once()
	helmClient.On("ReleaseExists", ctx, mock.Anything, mock.Anything).Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	spec, err := d.DetectComponents(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.NotNil(t, spec)
}

// TestDetectComponents_ArgoCD_MultiNamespace is a regression test for the Helm
// v4 AllNamespaces scoping bug, which is fixed by correctly re-initializing
// actionConfig. ArgoCD is installed in the "argocd" namespace, not "default"
// — if ListReleases only returns releases from a single namespace, ArgoCD
// will be missed and DetectComponents will incorrectly return GitOpsEngineNone.
func TestDetectComponents_ArgoCD_MultiNamespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	// ListReleases returns the ArgoCD release in the "argocd" namespace — a
	// non-default namespace. DetectComponents must correctly identify it.
	helmClient.On("ListReleases", ctx).Return([]helm.ReleaseInfo{
		{Name: detector.ReleaseArgoCD, Namespace: detector.NamespaceArgoCD},
	}, nil).Once()

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	spec, err := d.DetectComponents(ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	require.NoError(t, err)
	assert.NotNil(t, spec)
	assert.Equal(t, v1alpha1.GitOpsEngineArgoCD, spec.GitOpsEngine)
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

// --- Node Autoscaler Detection ---

func TestDetectNodeAutoscaler_Enabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(true, nil)

	helmClient.On("GetReleaseValues", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(map[string]any{
		"extraArgs": map[string]any{
			"expander":                         "price",
			"max-nodes-total":                  float64(10),
			"scale-down-unneeded-time":         "10m",
			"scale-down-utilization-threshold": "0.4",
		},
		"autoscalingGroups": []any{
			map[string]any{
				"name":         "autoscale-small",
				"instanceType": "cx23",
				"region":       "fsn1",
				"minSize":      float64(0),
				"maxSize":      float64(1),
			},
		},
	}, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cfg, err := d.ExportDetectNodeAutoscaler(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.NodeAutoscalerEnabledEnabled, cfg.Enabled)
	assert.Equal(t, v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderPrice}, cfg.Expander)
	assert.Equal(t, int32(10), cfg.MaxNodesTotal)
	assert.Equal(t, "10m", cfg.ScaleDownUnneededTime)
	assert.Equal(t, "0.4", cfg.ScaleDownUtilizationThreshold)
	require.Len(t, cfg.Pools, 1)
	assert.Equal(t, "autoscale-small", cfg.Pools[0].Name)
	assert.Equal(t, "cx23", cfg.Pools[0].ServerType)
	assert.Equal(t, "fsn1", cfg.Pools[0].Location)
	assert.Equal(t, int32(0), cfg.Pools[0].Min)
	assert.Equal(t, int32(1), cfg.Pools[0].Max)
}

func TestDetectNodeAutoscaler_CapacityBuffers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(true, nil)

	helmClient.On("GetReleaseValues", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(map[string]any{
		"extraArgs": map[string]any{
			"capacity-buffer-controller-enabled": true,
		},
	}, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cfg, err := d.ExportDetectNodeAutoscaler(ctx)

	require.NoError(t, err)
	assert.True(t, cfg.CapacityBuffers,
		"capacity-buffer-controller-enabled extraArg should round-trip to CapacityBuffers")
}

func TestDetectNodeAutoscaler_SkipNodesAndDaemonsets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(true, nil)

	helmClient.On("GetReleaseValues", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(map[string]any{
		"extraArgs": map[string]any{
			"ignore-daemonsets-utilization": true,
			"skip-nodes-with-local-storage": false,
			"skip-nodes-with-system-pods":   true,
		},
	}, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cfg, err := d.ExportDetectNodeAutoscaler(ctx)

	require.NoError(t, err)
	assert.True(t, cfg.IgnoreDaemonsetsUtilization,
		"ignore-daemonsets-utilization extraArg should round-trip to IgnoreDaemonsetsUtilization")
	require.NotNil(t, cfg.SkipNodesWithLocalStorage,
		"an explicit skip-nodes-with-local-storage should round-trip to a non-nil pointer")
	assert.False(t, *cfg.SkipNodesWithLocalStorage,
		"skip-nodes-with-local-storage=false should round-trip to a *false pointer")
	require.NotNil(t, cfg.SkipNodesWithSystemPods,
		"an explicit skip-nodes-with-system-pods should round-trip to a non-nil pointer")
	assert.True(t, *cfg.SkipNodesWithSystemPods,
		"skip-nodes-with-system-pods=true should round-trip to a *true pointer")
}

func TestDetectNodeAutoscaler_SkipNodesUnsetStayNil(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(true, nil)

	helmClient.On("GetReleaseValues", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(map[string]any{
		"extraArgs": map[string]any{},
	}, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cfg, err := d.ExportDetectNodeAutoscaler(ctx)

	require.NoError(t, err)
	assert.False(t, cfg.IgnoreDaemonsetsUtilization,
		"an absent ignore-daemonsets-utilization should leave the zero value (false)")
	assert.Nil(t, cfg.SkipNodesWithLocalStorage,
		"an absent skip-nodes-with-local-storage should leave a nil pointer (upstream default)")
	assert.Nil(t, cfg.SkipNodesWithSystemPods,
		"an absent skip-nodes-with-system-pods should leave a nil pointer (upstream default)")
}

func TestDetectNodeAutoscaler_NotInstalled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(false, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cfg, err := d.ExportDetectNodeAutoscaler(ctx)

	require.NoError(t, err)
	assert.False(t, cfg.Enabled.IsEnabled())
	assert.Empty(t, cfg.Pools)
}

func TestDetectNodeAutoscaler_ValuesUnreadable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(true, nil)

	helmClient.On("GetReleaseValues", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(nil, errValues)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cfg, err := d.ExportDetectNodeAutoscaler(ctx)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.NodeAutoscalerEnabledEnabled, cfg.Enabled)
	assert.Empty(t, cfg.Pools)
}

func TestDetectNodeAutoscaler_SkipsPoolWithEmptyName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	helmClient := helm.NewMockInterface(t)
	k8sClientset := fake.NewClientset()

	helmClient.On("ReleaseExists", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(true, nil)

	helmClient.On("GetReleaseValues", ctx,
		detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
	).Return(map[string]any{
		"autoscalingGroups": []any{
			map[string]any{
				"name":         "",
				"instanceType": "cx23",
				"region":       "fsn1",
				"minSize":      float64(0),
				"maxSize":      float64(1),
			},
			map[string]any{
				"instanceType": "cx33",
				"region":       "fsn1",
				"minSize":      float64(0),
				"maxSize":      float64(3),
			},
			map[string]any{
				"name":         "valid-pool",
				"instanceType": "cx43",
				"region":       "fsn1",
				"minSize":      float64(0),
				"maxSize":      float64(5),
			},
		},
	}, nil)

	d := detector.NewComponentDetector(helmClient, k8sClientset, nil)
	cfg, err := d.ExportDetectNodeAutoscaler(ctx)

	require.NoError(t, err)
	require.Len(t, cfg.Pools, 1)
	assert.Equal(t, "valid-pool", cfg.Pools[0].Name)
}

func TestHelmExpanderToEnum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected v1alpha1.AutoscalerExpander
	}{
		{"price", v1alpha1.AutoscalerExpanderPrice},
		{"least-waste", v1alpha1.AutoscalerExpanderLeastWaste},
		{"least-nodes", v1alpha1.AutoscalerExpanderLeastNodes},
		{"random", v1alpha1.AutoscalerExpanderRandom},
		{"unknown", v1alpha1.AutoscalerExpanderLeastWaste},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, detector.ExportHelmExpanderToEnum(tc.input))
		})
	}
}

func TestHelmExpandersToEnum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected v1alpha1.AutoscalerExpanderList
	}{
		{"empty", "", nil},
		{
			"single",
			"least-waste",
			v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
		},
		{
			"priority_list",
			"least-nodes,least-waste",
			v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderLeastNodes,
				v1alpha1.AutoscalerExpanderLeastWaste,
			},
		},
		{
			"priority_list_with_spaces",
			"least-nodes, price",
			v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderLeastNodes,
				v1alpha1.AutoscalerExpanderPrice,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, detector.ExportHelmExpandersToEnum(tc.input))
		})
	}
}
