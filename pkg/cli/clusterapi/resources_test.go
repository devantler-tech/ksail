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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

const kindPod = "Pod"

func testPod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{Kind: kindPod, APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
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
