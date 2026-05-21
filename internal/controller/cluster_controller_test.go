package controller_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var errBoom = errors.New("boom")

// fakeProvisioner is a configurable test double for clusterprovisioner.Provisioner.
type fakeProvisioner struct {
	exists      bool
	existsErr   error
	createErr   error
	deleteErr   error
	createCalls int
	deleteCalls int
}

func (f *fakeProvisioner) Create(_ context.Context, _ string) error {
	f.createCalls++

	return f.createErr
}

func (f *fakeProvisioner) Delete(_ context.Context, _ string) error {
	f.deleteCalls++

	return f.deleteErr
}

func (f *fakeProvisioner) Start(_ context.Context, _ string) error { return nil }
func (f *fakeProvisioner) Stop(_ context.Context, _ string) error  { return nil }

func (f *fakeProvisioner) List(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeProvisioner) Exists(_ context.Context, _ string) (bool, error) {
	return f.exists, f.existsErr
}

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	return scheme
}

func newCluster(withFinalizer bool) *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "default", Generation: 1},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionVCluster},
		},
	}
	if withFinalizer {
		cluster.Finalizers = []string{controller.FinalizerName}
	}

	return cluster
}

func newFakeClient(scheme *runtime.Scheme, cluster *v1alpha1.Cluster) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(&v1alpha1.Cluster{}).
		Build()
}

func newReconciler(
	scheme *runtime.Scheme,
	cl client.Client,
	prov *fakeProvisioner,
) *controller.ClusterReconciler {
	return &controller.ClusterReconciler{
		Client: cl,
		Scheme: scheme,
		NewProvisioner: func(
			_ context.Context,
			_ *v1alpha1.Cluster,
		) (clusterprovisioner.Provisioner, error) {
			return prov, nil
		},
	}
}

func request() ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: "c1", Namespace: "default"}}
}

func TestReconcile_AddsFinalizer(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	fakeClient := newFakeClient(scheme, newCluster(false))
	reconciler := newReconciler(scheme, fakeClient, &fakeProvisioner{})

	_, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), request().NamespacedName, &got))
	assert.True(t, containsFinalizer(got.Finalizers), "finalizer should be added")
}

func TestReconcile_CreatesWhenMissing(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	fakeClient := newFakeClient(scheme, newCluster(true))
	prov := &fakeProvisioner{exists: false}
	reconciler := newReconciler(scheme, fakeClient, prov)

	_, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, 1, prov.createCalls, "Create should be called once")

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), request().NamespacedName, &got))
	assert.Equal(t, v1alpha1.ClusterPhaseReady, got.Status.Phase)
	assert.Equal(t, int64(1), got.Status.ObservedGeneration)

	ready := apimeta.FindStatusCondition(got.Status.Conditions, v1alpha1.ConditionReady)
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionTrue, ready.Status)
}

func TestReconcile_SkipsCreateWhenExists(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	fakeClient := newFakeClient(scheme, newCluster(true))
	prov := &fakeProvisioner{exists: true}
	reconciler := newReconciler(scheme, fakeClient, prov)

	_, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, 0, prov.createCalls, "Create should not be called when the cluster exists")

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), request().NamespacedName, &got))
	assert.Equal(t, v1alpha1.ClusterPhaseReady, got.Status.Phase)
}

func TestReconcile_RecordsFailure(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	fakeClient := newFakeClient(scheme, newCluster(true))
	prov := &fakeProvisioner{existsErr: errBoom}
	reconciler := newReconciler(scheme, fakeClient, prov)

	res, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)
	assert.Positive(t, res.RequeueAfter, "failures should be requeued")

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), request().NamespacedName, &got))
	assert.Equal(t, v1alpha1.ClusterPhaseFailed, got.Status.Phase)

	degraded := apimeta.FindStatusCondition(got.Status.Conditions, v1alpha1.ConditionDegraded)
	require.NotNil(t, degraded)
	assert.Equal(t, metav1.ConditionTrue, degraded.Status)
}

func TestReconcile_DeletesAndRemovesFinalizer(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	cluster := newCluster(true)
	fakeClient := newFakeClient(scheme, cluster)
	prov := &fakeProvisioner{exists: true}
	reconciler := newReconciler(scheme, fakeClient, prov)

	// Deleting an object that still has a finalizer sets DeletionTimestamp without removing it.
	require.NoError(t, fakeClient.Delete(context.Background(), cluster))

	_, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, 1, prov.deleteCalls, "Delete should be called once")

	var got v1alpha1.Cluster

	getErr := fakeClient.Get(context.Background(), request().NamespacedName, &got)
	assert.True(t, apierrors.IsNotFound(getErr), "cluster should be gone after finalizer removal")
}

func containsFinalizer(finalizers []string) bool {
	for _, f := range finalizers {
		if f == controller.FinalizerName {
			return true
		}
	}

	return false
}
