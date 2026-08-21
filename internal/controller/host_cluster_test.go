package controller_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/controller"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	hostName      = "host"
	hostNamespace = "default"
)

// newHostCluster returns a host-labelled Cluster, mirroring the operator's self-registration of the
// cluster it runs on (empty spec, host-cluster label).
func newHostCluster(withFinalizer bool) *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       hostName,
			Namespace:  hostNamespace,
			Generation: 1,
			Labels:     map[string]string{v1alpha1.HostClusterLabel: "true"},
		},
	}
	if withFinalizer {
		cluster.Finalizers = []string{controller.FinalizerName}
	}

	return cluster
}

func hostRequest() ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{Name: hostName, Namespace: hostNamespace},
	}
}

// newHostReconciler builds a reconciler that enforces the reserved host-cluster invariant: the host
// path must never construct a provisioner (NewProvisioner fails the reconcile if invoked) nor install
// components (InstallComponents fails the test if invoked). The host observer reports fixed status.
func newHostReconciler(
	t *testing.T,
	fakeClient client.Client,
) *controller.ClusterReconciler {
	t.Helper()

	return &controller.ClusterReconciler{
		Client: fakeClient,
		Scheme: newScheme(t),
		NewProvisioner: func(
			_ context.Context,
			_ *v1alpha1.Cluster,
		) (clusterprovisioner.Provisioner, error) {
			return nil, errBoom
		},
		InstallComponents: func(
			_ context.Context,
			_ clusterprovisioner.Provisioner,
			_ *v1alpha1.Cluster,
		) (bool, []v1alpha1.ComponentStatus, error) {
			t.Error("the host cluster must never get components installed")

			return false, nil, errBoom
		},
		HostClusterNamespace: hostNamespace,
		ObserveHostStatus: func(
			_ context.Context,
			_ client.Reader,
			_ *v1alpha1.Cluster,
		) (controller.ObservedStatus, error) {
			return controller.ObservedStatus{
				Endpoint:      "https://10.96.0.1:443",
				NodesReady:    2,
				NodesTotal:    3,
				NodesObserved: true,
			}, nil
		},
	}
}

func TestReconcile_HostClusterReportsReadyWithoutProvisioner(t *testing.T) {
	t.Parallel()

	fakeClient := newFakeClient(newScheme(t), newHostCluster(false))
	reconciler := newHostReconciler(t, fakeClient)

	res, err := reconciler.Reconcile(context.Background(), hostRequest())
	require.NoError(t, err, "the host path must not build a provisioner")
	assert.Positive(t, res.RequeueAfter, "the host cluster should be re-observed periodically")

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(context.Background(), hostRequest().NamespacedName, &got))
	assert.Equal(t, v1alpha1.ClusterPhaseReady, got.Status.Phase)
	assert.Empty(t, got.Finalizers, "the host cluster must not get the teardown finalizer")
	assert.Equal(t, "https://10.96.0.1:443", got.Status.Endpoint)
	assert.Equal(t, int32(2), got.Status.NodesReady)
	assert.Equal(t, int32(3), got.Status.NodesTotal)

	ready := apimeta.FindStatusCondition(got.Status.Conditions, v1alpha1.ConditionReady)
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionTrue, ready.Status)

	components := apimeta.FindStatusCondition(
		got.Status.Conditions,
		v1alpha1.ConditionComponentsReady,
	)
	require.NotNil(t, components)
	assert.Equal(t, metav1.ConditionUnknown, components.Status)
	assert.Equal(t, "HostCluster", components.Reason)

	// The host cluster carries an empty spec, so no CLI-only fields are set.
	ignored := apimeta.FindStatusCondition(got.Status.Conditions, v1alpha1.ConditionIgnoredFields)
	require.NotNil(t, ignored)
	assert.Equal(t, metav1.ConditionFalse, ignored.Status)
}

func TestReconcile_ForgedHostLabelUsesNormalDelete(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	cluster := newHostCluster(true)
	cluster.Name = "evil-alias"
	fakeClient := newFakeClient(scheme, cluster)
	prov := &fakeProvisioner{exists: true}
	reconciler := newReconciler(scheme, fakeClient, prov)
	reconciler.HostClusterNamespace = hostNamespace

	require.NoError(t, fakeClient.Delete(context.Background(), cluster))

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, prov.deleteCalls, "forged host labels must not bypass normal teardown")
}

func TestReconcile_HostClusterDeleteSkipsProvisioner(t *testing.T) {
	t.Parallel()

	scheme := newScheme(t)
	// A user may have labelled an existing cluster that already carried the finalizer; deletion must
	// remove the finalizer without ever invoking the provisioner.
	cluster := newHostCluster(true)
	fakeClient := newFakeClient(scheme, cluster)
	prov := &fakeProvisioner{exists: true}
	reconciler := newReconciler(scheme, fakeClient, prov)
	reconciler.HostClusterNamespace = hostNamespace

	require.NoError(t, fakeClient.Delete(context.Background(), cluster))

	_, err := reconciler.Reconcile(context.Background(), hostRequest())
	require.NoError(t, err)
	assert.Equal(
		t,
		0,
		prov.deleteCalls,
		"deleting the host registration must not tear anything down",
	)

	var got v1alpha1.Cluster

	getErr := fakeClient.Get(context.Background(), hostRequest().NamespacedName, &got)
	assert.True(
		t,
		apierrors.IsNotFound(getErr),
		"the registration should be gone after finalizer removal",
	)
}
