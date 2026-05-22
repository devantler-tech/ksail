package controller_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
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

// fakeUpdaterProvisioner adds the optional Updater interface to fakeProvisioner so drift
// detection can be exercised. changes controls how many in-place changes DiffConfig reports.
type fakeUpdaterProvisioner struct {
	*fakeProvisioner

	changes     int
	updateCalls int
}

func (f *fakeUpdaterProvisioner) DiffConfig(
	_ context.Context,
	_ string,
	_, _ *v1alpha1.ClusterSpec,
) (*clusterupdate.UpdateResult, error) {
	result := clusterupdate.NewEmptyUpdateResult()
	for range f.changes {
		result.InPlaceChanges = append(
			result.InPlaceChanges,
			clusterupdate.Change{Field: "cluster.cni"},
		)
	}

	return result, nil
}

func (f *fakeUpdaterProvisioner) Update(
	_ context.Context,
	_ string,
	_, _ *v1alpha1.ClusterSpec,
	_ clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	f.updateCalls++

	return clusterupdate.NewEmptyUpdateResult(), nil
}

func (f *fakeUpdaterProvisioner) GetCurrentConfig(
	_ context.Context,
	_ string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	return nil, nil, nil
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

func newReconcilerWith(
	scheme *runtime.Scheme,
	cl client.Client,
	prov clusterprovisioner.Provisioner,
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

func newReconciler(
	scheme *runtime.Scheme,
	cl client.Client,
	prov *fakeProvisioner,
) *controller.ClusterReconciler {
	return newReconcilerWith(scheme, cl, prov)
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
	assert.True(
		t,
		slices.Contains(got.Finalizers, controller.FinalizerName),
		"finalizer should be added",
	)
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

func TestReconcile_DetectsDriftAndUpdates(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	cluster := newCluster(true)
	cluster.Annotations = map[string]string{controller.LastAppliedSpecAnnotation: "{}"}
	fakeClient := newFakeClient(scheme, cluster)
	prov := &fakeUpdaterProvisioner{fakeProvisioner: &fakeProvisioner{exists: true}, changes: 1}
	reconciler := newReconcilerWith(scheme, fakeClient, prov)

	_, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, 1, prov.updateCalls, "Update should be applied when drift is detected")

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), request().NamespacedName, &got))
	assert.Equal(t, v1alpha1.ClusterPhaseReady, got.Status.Phase)
}

func TestReconcile_NoDriftSkipsUpdate(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	cluster := newCluster(true)
	cluster.Annotations = map[string]string{controller.LastAppliedSpecAnnotation: "{}"}
	fakeClient := newFakeClient(scheme, cluster)
	prov := &fakeUpdaterProvisioner{fakeProvisioner: &fakeProvisioner{exists: true}, changes: 0}
	reconciler := newReconcilerWith(scheme, fakeClient, prov)

	_, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, 0, prov.updateCalls, "Update should not run when there is no drift")
}

func TestProvisionedName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		namespace string
		name      string
		want      string
	}{
		"namespaced": {namespace: "team-a", name: "c1", want: "team-a-c1"},
		"different namespace same name": {
			namespace: "team-b",
			name:      "c1",
			want:      "team-b-c1",
		},
		"empty namespace falls back to name": {namespace: "", name: "c1", want: "c1"},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			cluster := &v1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: testCase.name, Namespace: testCase.namespace},
			}

			assert.Equal(t, testCase.want, controller.ProvisionedName(cluster))
		})
	}
}

func TestProvisionedNameTruncatesLongNames(t *testing.T) {
	t.Parallel()

	cluster := &v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Repeat("b", 40),
			Namespace: strings.Repeat("a", 40),
		},
	}

	got := controller.ProvisionedName(cluster)

	assert.LessOrEqual(t, len(got), 54)
	assert.Equal(t, got, controller.ProvisionedName(cluster), "must be deterministic")
	assert.False(t, strings.HasPrefix(got, "-"))
	assert.False(t, strings.HasSuffix(got, "-"))
	// vcluster derives a namespace "vcluster-<name>"; it must fit the 63-char DNS-1123 label limit.
	assert.LessOrEqual(t, len("vcluster-"+got), 63)
}

func TestReconcile_AppliesObservedStatus(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	fakeClient := newFakeClient(scheme, newCluster(true))
	reconciler := newReconciler(scheme, fakeClient, &fakeProvisioner{exists: true})
	reconciler.ObserveStatus = func(
		_ context.Context,
		_ client.Reader,
		_ *v1alpha1.Cluster,
	) (controller.ObservedStatus, error) {
		return controller.ObservedStatus{
			Endpoint:         "https://child.svc:443",
			KubeconfigSecret: &v1alpha1.SecretReference{Name: "vc-c1", Namespace: "vcluster-c1"},
			NodesReady:       2,
			NodesTotal:       3,
			NodesObserved:    true,
		}, nil
	}

	_, err := reconciler.Reconcile(context.Background(), request())
	require.NoError(t, err)

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), request().NamespacedName, &got))
	assert.Equal(t, "https://child.svc:443", got.Status.Endpoint)
	require.NotNil(t, got.Status.KubeconfigSecretRef)
	assert.Equal(t, "vc-c1", got.Status.KubeconfigSecretRef.Name)
	assert.Equal(t, int32(2), got.Status.NodesReady)
	assert.Equal(t, int32(3), got.Status.NodesTotal)
}

func TestReconcile_ObserveStatusErrorIsBestEffort(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	fakeClient := newFakeClient(scheme, newCluster(true))
	reconciler := newReconciler(scheme, fakeClient, &fakeProvisioner{exists: true})
	// Observation fails to reach the child cluster but still derives the endpoint.
	reconciler.ObserveStatus = func(
		_ context.Context,
		_ client.Reader,
		_ *v1alpha1.Cluster,
	) (controller.ObservedStatus, error) {
		return controller.ObservedStatus{Endpoint: "https://child.svc:443"}, errBoom
	}

	_, err := reconciler.Reconcile(context.Background(), request())
	// A best-effort observation error must not fail the reconcile.
	require.NoError(t, err)

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), request().NamespacedName, &got))
	assert.Equal(t, v1alpha1.ClusterPhaseReady, got.Status.Phase)
	assert.Equal(t, "https://child.svc:443", got.Status.Endpoint)
	assert.Zero(t, got.Status.NodesTotal, "nodes stay unset when not observed")
}
