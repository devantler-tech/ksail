package clusterapi_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

const (
	kindPod        = "Pod"
	kindDeployment = "Deployment"
	nameWeb        = "web"
	nameApps       = "apps"
)

func testPod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{Kind: kindPod, APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

func testDeployment(namespace, name string, replicas int32) *appsv1.Deployment {
	count := replicas

	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{Kind: kindDeployment, APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       appsv1.DeploymentSpec{Replicas: &count},
	}
}

// testGitOpsCR builds an unstructured Flux/ArgoCD custom resource. The fake dynamic client guesses
// its GVR from the GVK (Kustomization→kustomizations, Application→applications), matching the
// allowlist mapping, so reconcile (a merge-patch) round-trips without registering real CRD schemes.
func testGitOpsCR(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
	}}
}

func injectFakeDynamic(service *clusterapi.Service, objects ...runtime.Object) {
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, objects...)

	service.SetDynamicClientForTest(
		func(_ context.Context, _ string) (dynamic.Interface, error) { return client, nil },
	)
}

func TestListResourcesFiltersByNamespace(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service, testPod("x", "p1"), testPod("y", "p2"))

	all, err := service.ListResources(
		context.Background(), "default", "c1", api.ResourceQuery{Kind: kindPod},
	)
	require.NoError(t, err)
	assert.Len(t, all.Items, 2)

	one, err := service.ListResources(
		context.Background(), "default", "c1", api.ResourceQuery{Kind: kindPod, Namespace: "x"},
	)
	require.NoError(t, err)
	require.Len(t, one.Items, 1)
	assert.Equal(t, "p1", one.Items[0].GetName())
}

func TestGetResource(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service, testPod("x", "p1"))

	obj, err := service.GetResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindPod, Namespace: "x", Name: "p1"},
	)
	require.NoError(t, err)
	assert.Equal(t, "p1", obj.GetName())
}

func TestListResourcesRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service)

	_, err := service.ListResources(
		context.Background(), "default", "c1", api.ResourceQuery{Kind: "Secret"},
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestScaleResource(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service, testDeployment("x", nameWeb, 1))

	err := service.ScaleResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindDeployment, Namespace: "x", Name: nameWeb}, 3,
	)
	require.NoError(t, err)

	obj, err := service.GetResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindDeployment, Namespace: "x", Name: nameWeb},
	)
	require.NoError(t, err)

	replicas, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(3), replicas)
}

func TestScaleRejectsNonScalableKind(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service, testPod("x", "p1"))

	err := service.ScaleResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindPod, Namespace: "x", Name: "p1"}, 2,
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestRestartResourceStampsAnnotation(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service, testDeployment("x", nameWeb, 1))

	err := service.RestartResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindDeployment, Namespace: "x", Name: nameWeb},
	)
	require.NoError(t, err)

	obj, err := service.GetResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindDeployment, Namespace: "x", Name: nameWeb},
	)
	require.NoError(t, err)

	stamp, found, err := unstructured.NestedString(
		obj.Object,
		"spec",
		"template",
		"metadata",
		"annotations",
		"kubectl.kubernetes.io/restartedAt",
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.NotEmpty(t, stamp)
}

func TestReconcileRejectsNonReconcilableKind(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service, testPod("x", "p1"))

	// Reconcile is only valid for GitOps CRs (Flux/ArgoCD); a Pod is rejected before any client call.
	err := service.ReconcileResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindPod, Namespace: "x", Name: "p1"},
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestReconcileResourceStampsFluxAnnotation(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(
		service,
		testGitOpsCR("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", nameApps),
	)

	err := service.ReconcileResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: "Kustomization", Namespace: "flux-system", Name: nameApps},
	)
	require.NoError(t, err)

	obj, err := service.GetResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: "Kustomization", Namespace: "flux-system", Name: nameApps},
	)
	require.NoError(t, err)

	// Flux watches reconcile.fluxcd.io/requestedAt; the stamp is a non-empty RFC3339Nano timestamp.
	stamp, found, err := unstructured.NestedString(
		obj.Object, "metadata", "annotations", "reconcile.fluxcd.io/requestedAt",
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.NotEmpty(t, stamp)
}

func TestReconcileResourceStampsArgoCDAnnotation(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(
		service,
		testGitOpsCR("argoproj.io/v1alpha1", "Application", "argocd", nameApps),
	)

	err := service.ReconcileResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: "Application", Namespace: "argocd", Name: nameApps},
	)
	require.NoError(t, err)

	obj, err := service.GetResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: "Application", Namespace: "argocd", Name: nameApps},
	)
	require.NoError(t, err)

	// ArgoCD refreshes on argocd.argoproj.io/refresh=normal (not a timestamp like Flux).
	refresh, found, err := unstructured.NestedString(
		obj.Object, "metadata", "annotations", "argocd.argoproj.io/refresh",
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "normal", refresh)
}

func TestRestartRejectsNonRestartableKind(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service, testPod("x", "p1"))

	err := service.RestartResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindPod, Namespace: "x", Name: "p1"},
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestDeleteResource(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service, testPod("x", "p1"))

	err := service.DeleteResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindPod, Namespace: "x", Name: "p1"},
	)
	require.NoError(t, err)

	list, err := service.ListResources(
		context.Background(), "default", "c1", api.ResourceQuery{Kind: kindPod},
	)
	require.NoError(t, err)
	assert.Empty(t, list.Items)
}

func TestDeleteRejectsClusterScopedKind(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service)

	// Node is cluster-scoped: deletion is intentionally not allowed from the workload browser.
	err := service.DeleteResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: "Node", Name: "node-1"},
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestScaleRejectsMissingNamespace(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	injectFakeDynamic(service)

	// A namespaced kind addressed without a namespace is rejected as invalid (422), not an opaque 500.
	err := service.ScaleResource(
		context.Background(), "default", "c1",
		api.ResourceRef{Kind: kindDeployment, Name: nameWeb}, 2,
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

const kindProdKubeconfig = `apiVersion: v1
kind: Config
current-context: kind-prod
clusters:
  - name: kind-prod
    cluster:
      server: https://127.0.0.1:6443
contexts:
  - name: kind-prod
    context:
      cluster: kind-prod
      user: kind-prod
users:
  - name: kind-prod
    user: {}
`

// errRESTConfig is the sentinel a fake restConfigForCluster seam returns to prove every derived
// client builder funnels through that single seam.
var errRESTConfig = errors.New("rest config unavailable")

// TestRESTConfigSeamFeedsEveryDefaultClient asserts the single restConfigForCluster seam is the source
// of all four default client builders: when the seam fails, the dynamic read path surfaces exactly
// that error rather than reaching a real kubeconfig.
func TestRESTConfigSeamFeedsEveryDefaultClient(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)
	service.SetRESTConfigForClusterForTest(func(string) (*rest.Config, error) {
		return nil, errRESTConfig
	})

	_, err := service.ListResources(
		context.Background(), "default", "c1", api.ResourceQuery{Kind: kindPod},
	)
	require.ErrorIs(t, err, errRESTConfig,
		"the default dynamic client must derive from the single restConfigForCluster seam")
}

func TestContextForCluster(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(kindProdKubeconfig), 0o600))

	// "kind-prod" context detects to cluster name "prod" (Kind's kind-<name> pattern).
	contextName, err := clusterapi.ContextForCluster(path, "prod")
	require.NoError(t, err)
	assert.Equal(t, "kind-prod", contextName)

	_, err = clusterapi.ContextForCluster(path, "missing")
	require.ErrorIs(t, err, api.ErrNotFound)
}
