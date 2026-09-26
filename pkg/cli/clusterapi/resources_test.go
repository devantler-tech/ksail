package clusterapi_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
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

	// A managed row addressed by its full context name resolves through the raw-name fallback:
	// detection yields "prod" for kind-prod, so the detected-name pass alone would reject it.
	contextName, err = clusterapi.ContextForCluster(path, "kind-prod")
	require.NoError(t, err)
	assert.Equal(t, "kind-prod", contextName)

	_, err = clusterapi.ContextForCluster(path, "missing")
	require.ErrorIs(t, err, api.ErrNotFound)
}

// unmanagedContextKubeconfig carries a managed kind-prod context plus an unmanaged context whose name
// follows no distribution pattern (an EKS/kubeadm/colleague cluster), the shape List surfaces as an
// unmanaged row keyed by the raw context name.
const unmanagedContextKubeconfig = `apiVersion: v1
kind: Config
current-context: kind-prod
clusters:
  - name: kind-prod
    cluster:
      server: https://127.0.0.1:6443
  - name: colleague
    cluster:
      server: https://cluster.example.com:6443
contexts:
  - name: kind-prod
    context:
      cluster: kind-prod
      user: kind-prod
  - name: colleague-cluster
    context:
      cluster: colleague
      user: colleague
users:
  - name: kind-prod
    user: {}
  - name: colleague
    user: {}
`

// TestContextForClusterResolvesUnmanagedRawContextName pins the fix for #6116: an unmanaged
// kubeconfig context is listed under its raw context name, and every resource operation must
// resolve that same name back to the context instead of reporting 404 for a row List advertised.
func TestContextForClusterResolvesUnmanagedRawContextName(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(unmanagedContextKubeconfig), 0o600))

	contextName, err := clusterapi.ContextForCluster(path, "colleague-cluster")
	require.NoError(t, err)
	assert.Equal(t, "colleague-cluster", contextName)

	// The raw-name fallback is exact: a substring or the cluster entry's name is not a context.
	_, err = clusterapi.ContextForCluster(path, "colleague")
	require.ErrorIs(t, err, api.ErrNotFound)
}

// collidingContextsKubeconfig holds a context literally named "prod" beside "kind-prod", whose
// detected cluster name is also "prod".
const collidingContextsKubeconfig = `apiVersion: v1
kind: Config
clusters:
  - name: kind-prod
    cluster:
      server: https://127.0.0.1:6443
  - name: stray
    cluster:
      server: https://127.0.0.1:6444
contexts:
  - name: kind-prod
    context:
      cluster: kind-prod
      user: kind-prod
  - name: prod
    context:
      cluster: stray
      user: stray
users:
  - name: kind-prod
    user: {}
  - name: stray
    user: {}
`

// TestContextForClusterUnmanagedRowsResolveToTheirOwnContext pins #6909: when neither "prod" nor
// "kind-prod" is managed, List shows both as unmanaged rows, and each must operate on its own context.
func TestContextForClusterUnmanagedRowsResolveToTheirOwnContext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(collidingContextsKubeconfig), 0o600))

	for _, row := range []string{"prod", "kind-prod"} {
		contextName, err := clusterapi.ContextForCluster(path, row)
		require.NoError(t, err)
		assert.Equal(t, row, contextName)
	}
}

// TestContextForClusterDetectedNameKeepsPrecedence pins that a managed cluster keeps resolving to its
// distribution context even when a stray context is literally named after it: List hides that stray
// context behind the managed row, so the name can only mean the managed cluster.
func TestContextForClusterDetectedNameKeepsPrecedence(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(collidingContextsKubeconfig), 0o600))

	contextName, err := clusterapi.ContextForCluster(path, "prod", "prod")
	require.NoError(t, err)
	assert.Equal(t, "kind-prod", contextName)
}

// TestContextForClusterReportsAmbiguousName pins that a name several contexts detect to, with no row
// of its own, is refused rather than resolved to whichever context the map yields first.
func TestContextForClusterReportsAmbiguousName(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
clusters:
  - name: kind-prod
    cluster:
      server: https://127.0.0.1:6443
  - name: k3d-prod
    cluster:
      server: https://127.0.0.1:6445
contexts:
  - name: kind-prod
    context:
      cluster: kind-prod
      user: kind-prod
  - name: k3d-prod
    context:
      cluster: k3d-prod
      user: k3d-prod
users:
  - name: kind-prod
    user: {}
  - name: k3d-prod
    user: {}
`), 0o600))

	for _, managed := range [][]string{nil, {"prod"}} {
		_, err := clusterapi.ContextForCluster(path, "prod", managed...)
		require.ErrorIs(t, err, clusterapi.ErrAmbiguousClusterContext)
		require.ErrorContains(t, err, "k3d-prod, kind-prod")
	}

	for _, contextName := range []string{"kind-prod", "k3d-prod"} {
		resolved, err := clusterapi.ContextForCluster(path, contextName)
		require.NoError(t, err)
		assert.Equal(t, contextName, resolved)
	}
}

// TestListedRowsResolveToTheirOwnEndpoint drives the production seam for every row List shows and
// asserts it reaches the endpoint that row reports, whether or not "prod" is a managed cluster.
func TestListedRowsResolveToTheirOwnEndpoint(t *testing.T) {
	t.Parallel()

	for name, managed := range map[string][]string{
		"unmanaged": nil,
		"managed":   {"prod"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config")
			require.NoError(t, os.WriteFile(path, []byte(collidingContextsKubeconfig), 0o600))

			service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
				v1alpha1.DistributionVanilla: {clusters: managed},
			})
			service.SetKubeconfigPathForTest(path)

			list, err := service.List(t.Context())
			require.NoError(t, err)
			require.NotEmpty(t, list.Items)

			for _, row := range list.Items {
				restConfig, err := service.RESTConfigForClusterForTest(t.Context(), row.Name)
				require.NoError(t, err)
				assert.Equal(t, row.Status.Endpoint, restConfig.Host, "row %q", row.Name)
			}
		})
	}
}

// TestRESTConfigForUnmanagedClusterTargetsItsContext drives the production restConfigForCluster seam
// (the one every default client — dynamic, apply, log, exec, proxy, watch — derives from) for an
// unmanaged row and asserts the resulting REST config points at that context's API server. This is
// the user-visible half of #6116: the row List surfaces is now operable rather than a 404.
func TestRESTConfigForUnmanagedClusterTargetsItsContext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(unmanagedContextKubeconfig), 0o600))

	service := newTestService(nil)
	service.SetKubeconfigPathForTest(path)

	restConfig, err := service.RESTConfigForClusterForTest(t.Context(), "colleague-cluster")
	require.NoError(t, err)
	assert.Equal(t, "https://cluster.example.com:6443", restConfig.Host)

	// A name no context carries still reports not-found (→ 404) rather than falling back to the
	// current context and silently operating on the wrong cluster.
	_, err = service.RESTConfigForClusterForTest(t.Context(), "ghost")
	require.ErrorIs(t, err, api.ErrNotFound)
}

// homeKubeconfig writes a kubeconfig under a fresh home directory whose colleague-cluster context
// points at a different server than unmanagedContextKubeconfig's, so a test can tell which file was
// read.
func homeKubeconfig(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	path := filepath.Join(home, ".kube", "config")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(unmanagedContextKubeconfig,
		"cluster.example.com", "home.example.com")), 0o600))

	return home
}

// TestNewServiceReadsTheKubeconfigThatKUBECONFIGNames pins #6906: the web UI and desktop app resolve
// the kubeconfig as the CLI does, so a set KUBECONFIG wins over the home directory's file.
func TestNewServiceReadsTheKubeconfigThatKUBECONFIGNames(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(envPath, []byte(unmanagedContextKubeconfig), 0o600))
	t.Setenv("HOME", homeKubeconfig(t))
	t.Setenv("KUBECONFIG", envPath)

	restConfig, err := clusterapi.NewService().
		RESTConfigForClusterForTest(t.Context(), "colleague-cluster")
	require.NoError(t, err)
	assert.Equal(t, "https://cluster.example.com:6443", restConfig.Host)
}

// TestNewServiceReadsTheHomeKubeconfigWithoutKUBECONFIG pins the fallback: with KUBECONFIG unset the
// service still reads ~/.kube/config.
func TestNewServiceReadsTheHomeKubeconfigWithoutKUBECONFIG(t *testing.T) {
	t.Setenv("HOME", homeKubeconfig(t))
	t.Setenv("KUBECONFIG", "")

	restConfig, err := clusterapi.NewService().
		RESTConfigForClusterForTest(t.Context(), "colleague-cluster")
	require.NoError(t, err)
	assert.Equal(t, "https://home.example.com:6443", restConfig.Host)
}
