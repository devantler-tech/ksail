package clusterapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
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
)

const (
	kindPod        = "Pod"
	kindDeployment = "Deployment"
	nameWeb        = "web"
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
