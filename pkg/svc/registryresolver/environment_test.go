package registryresolver_test

import (
	"context"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	"github.com/devantler-tech/ksail/v7/pkg/svc/registryresolver"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDetectGitOpsEngine_ReleasePrimary(t *testing.T) {
	t.Parallel()

	helmClient := helm.NewMockInterface(t)
	helmClient.On("ReleaseExists", context.Background(),
		detector.ReleaseFluxOperator, detector.NamespaceFluxOperator).
		Return(true, nil).Maybe()

	engine, err := registryresolver.DetectGitOpsEngine(
		context.Background(),
		&registryresolver.Clients{HelmClient: helmClient},
	)
	require.NoError(t, err)
	require.Equal(t, v1alpha1.GitOpsEngineFlux, engine)
}

func TestDetectGitOpsEngine_NamespaceSecondaryWhenNoRelease(t *testing.T) {
	t.Parallel()

	// No KSail-managed Helm release (e.g. a plain `flux bootstrap` cluster), but
	// the flux-system namespace exists — must still detect as Flux via the
	// secondary namespace probe rather than regressing to None.
	helmClient := helm.NewMockInterface(t)
	helmClient.On("ReleaseExists", context.Background(),
		detector.ReleaseFluxOperator, detector.NamespaceFluxOperator).
		Return(false, nil).Maybe()
	helmClient.On("ReleaseExists", context.Background(),
		detector.ReleaseArgoCD, detector.NamespaceArgoCD).
		Return(false, nil).Maybe()

	clientset := fake.NewClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "flux-system"},
	})

	engine, err := registryresolver.DetectGitOpsEngine(
		context.Background(),
		&registryresolver.Clients{HelmClient: helmClient, KubernetesClient: clientset},
	)
	require.NoError(t, err)
	require.Equal(t, v1alpha1.GitOpsEngineFlux, engine)
}

func TestDetectGitOpsEngine_NoneWhenNeitherSignal(t *testing.T) {
	t.Parallel()

	helmClient := helm.NewMockInterface(t)
	helmClient.On("ReleaseExists", context.Background(),
		detector.ReleaseFluxOperator, detector.NamespaceFluxOperator).
		Return(false, nil).Maybe()
	helmClient.On("ReleaseExists", context.Background(),
		detector.ReleaseArgoCD, detector.NamespaceArgoCD).
		Return(false, nil).Maybe()

	clientset := fake.NewClientset()

	engine, err := registryresolver.DetectGitOpsEngine(
		context.Background(),
		&registryresolver.Clients{HelmClient: helmClient, KubernetesClient: clientset},
	)
	require.ErrorIs(t, err, registryresolver.ErrNoGitOpsEngine)
	require.Equal(t, v1alpha1.GitOpsEngineNone, engine)
}

func TestDetectGitOpsEngine_NamespaceSecondaryWhenHelmUnreadable(t *testing.T) {
	t.Parallel()

	// Helm history is unreadable (restricted RBAC); the namespace probe must
	// still classify a cluster with the argocd namespace as ArgoCD.
	clientset := fake.NewClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "argocd"},
	})

	// No HelmClient injected and no kubeconfig set, so the bundle would try to
	// build a real helm client; supply a nil-safe failing mock via ReleaseExists.
	helmClient := helm.NewMockInterface(t)
	helmClient.On("ReleaseExists", context.Background(),
		detector.ReleaseFluxOperator, detector.NamespaceFluxOperator).
		Return(false, assertHelmUnreadable()).Maybe()

	engine, err := registryresolver.DetectGitOpsEngine(
		context.Background(),
		&registryresolver.Clients{HelmClient: helmClient, KubernetesClient: clientset},
	)
	require.NoError(t, err)
	require.Equal(t, v1alpha1.GitOpsEngineArgoCD, engine)
}

// assertHelmUnreadable returns a sentinel error simulating restricted Helm RBAC.
func assertHelmUnreadable() error {
	return context.DeadlineExceeded
}
