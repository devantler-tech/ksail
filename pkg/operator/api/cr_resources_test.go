package api_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

const (
	kindConfigMap   = "ConfigMap"
	kindDeployment  = "Deployment"
	kindPod         = "Pod"
	nsDefault       = "default"
	configMapName   = "cfg"
	deploymentName  = "web"
	sampleClusterID = "c1"
)

func testDeployment(namespace, name string, replicas int32) *appsv1.Deployment {
	count := replicas

	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{Kind: kindDeployment, APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       appsv1.DeploymentSpec{Replicas: &count},
	}
}

// testGitOpsCR builds an unstructured Flux/ArgoCD custom resource. The fake dynamic client guesses its
// GVR from the GVK (Kustomization→kustomizations), matching the allowlist mapping, so a reconcile
// merge-patch round-trips without registering real CRD schemes.
func testGitOpsCR(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	obj.SetNamespace(namespace)
	obj.SetName(name)

	return obj
}

// newConnectedService builds a connected operator service with the sample Cluster CR present in the hub
// and the given fake dynamic client standing in for the cluster's managed child cluster.
func newConnectedService(t *testing.T, dyn dynamic.Interface) api.ClusterService {
	t.Helper()

	return api.NewCRClusterServiceWithResources(newClient(t, sampleCluster()), childResolver(dyn))
}

var errResolveBoom = errors.New("cannot reach child cluster")

// childResolver builds a child-cluster resolver that always yields the given dynamic client, so the
// operator ResourceService can be tested without the real vcluster connection logic.
func childResolver(
	dyn dynamic.Interface,
) func(context.Context, *v1alpha1.Cluster) (dynamic.Interface, error) {
	return func(context.Context, *v1alpha1.Cluster) (dynamic.Interface, error) {
		return dyn, nil
	}
}

func TestCRConnectedListsAndGetsChildResources(t *testing.T) {
	t.Parallel()

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: nsDefault},
	}
	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, configMap)

	service := api.NewCRClusterServiceWithResources(
		newClient(t, sampleCluster()),
		childResolver(dyn),
	)

	resourceService, ok := service.(api.ResourceService)
	require.True(t, ok, "connected operator service must implement ResourceService")

	list, err := resourceService.ListResources(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceQuery{Kind: kindConfigMap, Namespace: nsDefault},
	)
	require.NoError(t, err)
	assert.Len(t, list.Items, 1)

	obj, err := resourceService.GetResource(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceRef{Kind: kindConfigMap, Namespace: nsDefault, Name: configMapName},
	)
	require.NoError(t, err)
	assert.Equal(t, configMapName, obj.GetName())
}

func TestCRConnectedClusterNotFound(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	service := api.NewCRClusterServiceWithResources(
		newClient(t),
		childResolver(dyn),
	)
	resourceService, _ := service.(api.ResourceService)

	_, err := resourceService.ListResources(
		context.Background(), defaultNS, "missing",
		api.ResourceQuery{Kind: kindConfigMap},
	)
	require.Error(t, err, "listing for an absent Cluster CR must surface the get error")
}

func TestCRConnectedResolverError(t *testing.T) {
	t.Parallel()

	resolver := func(context.Context, *v1alpha1.Cluster) (dynamic.Interface, error) {
		return nil, errResolveBoom
	}
	service := api.NewCRClusterServiceWithResources(newClient(t, sampleCluster()), resolver)
	resourceService, _ := service.(api.ResourceService)

	_, err := resourceService.ListResources(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceQuery{Kind: kindConfigMap},
	)
	require.ErrorIs(t, err, errResolveBoom)
}

func TestCRConnectedUnknownKind(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	service := api.NewCRClusterServiceWithResources(
		newClient(t, sampleCluster()),
		childResolver(dyn),
	)
	resourceService, _ := service.(api.ResourceService)

	_, err := resourceService.ListResources(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceQuery{Kind: "NotARealKind"},
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

// TestCRPlainServiceHasNoResourceBrowser documents that the plain operator backend (no child-cluster
// resolver) does not advertise the resource browser — only the connected variant does.
func TestCRPlainServiceHasNoResourceBrowser(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newClient(t))
	_, ok := service.(api.ResourceService)
	assert.False(t, ok, "plain operator service must not implement ResourceService")
}

// TestCRPlainServiceHasNoResourceWriter documents that the plain operator backend does not advertise
// the write actions — only the connected variant (with a child-cluster resolver) does.
func TestCRPlainServiceHasNoResourceWriter(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newClient(t))
	_, ok := service.(api.ResourceWriter)
	assert.False(t, ok, "plain operator service must not implement ResourceWriter")
}

func TestCRConnectedScaleResource(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(
		clientgoscheme.Scheme,
		testDeployment(nsDefault, deploymentName, 1),
	)
	service := newConnectedService(t, dyn)

	writer, ok := service.(api.ResourceWriter)
	require.True(t, ok, "connected operator service must implement ResourceWriter")

	ref := api.ResourceRef{Kind: kindDeployment, Namespace: nsDefault, Name: deploymentName}
	require.NoError(
		t,
		writer.ScaleResource(context.Background(), defaultNS, sampleClusterID, ref, 4),
	)

	reader, _ := service.(api.ResourceService)

	obj, err := reader.GetResource(context.Background(), defaultNS, sampleClusterID, ref)
	require.NoError(t, err)

	replicas, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(4), replicas)
}

func TestCRConnectedScaleRejectsNonScalableKind(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	writer, _ := newConnectedService(t, dyn).(api.ResourceWriter)

	err := writer.ScaleResource(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceRef{Kind: kindPod, Namespace: nsDefault, Name: "p1"}, 2,
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestCRConnectedRestartStampsAnnotation(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(
		clientgoscheme.Scheme,
		testDeployment(nsDefault, deploymentName, 1),
	)
	service := newConnectedService(t, dyn)
	writer, _ := service.(api.ResourceWriter)

	ref := api.ResourceRef{Kind: kindDeployment, Namespace: nsDefault, Name: deploymentName}
	require.NoError(
		t,
		writer.RestartResource(context.Background(), defaultNS, sampleClusterID, ref),
	)

	reader, _ := service.(api.ResourceService)

	obj, err := reader.GetResource(context.Background(), defaultNS, sampleClusterID, ref)
	require.NoError(t, err)

	stamp, found, err := unstructured.NestedString(
		obj.Object, "spec", "template", "metadata", "annotations",
		"kubectl.kubernetes.io/restartedAt",
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.NotEmpty(t, stamp)
}

func TestCRConnectedDeleteResource(t *testing.T) {
	t.Parallel()

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: nsDefault},
	}
	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, configMap)
	service := newConnectedService(t, dyn)
	writer, _ := service.(api.ResourceWriter)

	ref := api.ResourceRef{Kind: kindConfigMap, Namespace: nsDefault, Name: configMapName}
	require.NoError(t, writer.DeleteResource(context.Background(), defaultNS, sampleClusterID, ref))

	reader, _ := service.(api.ResourceService)

	list, err := reader.ListResources(
		context.Background(), defaultNS, sampleClusterID, api.ResourceQuery{Kind: kindConfigMap},
	)
	require.NoError(t, err)
	assert.Empty(t, list.Items)
}

func TestCRConnectedDeleteRejectsClusterScopedKind(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	writer, _ := newConnectedService(t, dyn).(api.ResourceWriter)

	// Node is cluster-scoped: deletion is intentionally not allowed from the workload browser.
	err := writer.DeleteResource(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceRef{Kind: "Node", Name: "node-1"},
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestCRConnectedReconcileStampsFluxAnnotation(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(
		clientgoscheme.Scheme,
		testGitOpsCR("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "apps"),
	)
	service := newConnectedService(t, dyn)
	writer, _ := service.(api.ResourceWriter)

	ref := api.ResourceRef{Kind: "Kustomization", Namespace: "flux-system", Name: "apps"}
	require.NoError(
		t,
		writer.ReconcileResource(context.Background(), defaultNS, sampleClusterID, ref),
	)

	reader, _ := service.(api.ResourceService)

	obj, err := reader.GetResource(context.Background(), defaultNS, sampleClusterID, ref)
	require.NoError(t, err)

	stamp, found, err := unstructured.NestedString(
		obj.Object, "metadata", "annotations", "reconcile.fluxcd.io/requestedAt",
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.NotEmpty(t, stamp)
}

func TestCRConnectedReconcileRejectsNonReconcilableKind(t *testing.T) {
	t.Parallel()

	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	writer, _ := newConnectedService(t, dyn).(api.ResourceWriter)

	err := writer.ReconcileResource(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceRef{Kind: kindPod, Namespace: nsDefault, Name: "p1"},
	)
	require.ErrorIs(t, err, api.ErrInvalid)
}

// TestCRConnectedWriteResolverError confirms a failure to connect to the child cluster surfaces from a
// write action (not just the read paths).
func TestCRConnectedWriteResolverError(t *testing.T) {
	t.Parallel()

	resolver := func(context.Context, *v1alpha1.Cluster) (dynamic.Interface, error) {
		return nil, errResolveBoom
	}
	service := api.NewCRClusterServiceWithResources(newClient(t, sampleCluster()), resolver)
	writer, _ := service.(api.ResourceWriter)

	err := writer.ScaleResource(
		context.Background(), defaultNS, sampleClusterID,
		api.ResourceRef{Kind: kindDeployment, Namespace: nsDefault, Name: deploymentName}, 2,
	)
	require.ErrorIs(t, err, errResolveBoom)
}
