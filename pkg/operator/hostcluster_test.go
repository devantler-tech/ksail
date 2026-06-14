package operator_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const hostNamespace = "ksail-system"

func newHostFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func getHostCluster(t *testing.T, hub client.Client) *v1alpha1.Cluster {
	t.Helper()

	var cluster v1alpha1.Cluster

	key := types.NamespacedName{Namespace: hostNamespace, Name: operator.HostClusterName}
	require.NoError(t, hub.Get(context.Background(), key, &cluster))

	return &cluster
}

func TestEnsureHostClusterCreatesLabelledRegistration(t *testing.T) {
	t.Parallel()

	hub := newHostFakeClient(t)

	require.NoError(t, operator.EnsureHostCluster(context.Background(), hub, hostNamespace))

	cluster := getHostCluster(t, hub)
	assert.True(t, cluster.IsHostCluster(), "the registration must carry the host-cluster label")
	assert.Empty(t, cluster.Spec.Cluster.Distribution, "the host spec is intentionally empty")
}

func TestEnsureHostClusterIsIdempotent(t *testing.T) {
	t.Parallel()

	hub := newHostFakeClient(t)

	require.NoError(t, operator.EnsureHostCluster(context.Background(), hub, hostNamespace))
	require.NoError(t, operator.EnsureHostCluster(context.Background(), hub, hostNamespace))

	getHostCluster(t, hub)
}

func TestEnsureHostClusterNeverAdoptsUnlabelledCluster(t *testing.T) {
	t.Parallel()

	existing := &v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: operator.HostClusterName, Namespace: hostNamespace},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionVCluster},
		},
	}
	hub := newHostFakeClient(t, existing)

	err := operator.EnsureHostCluster(context.Background(), hub, hostNamespace)
	require.ErrorIs(t, err, operator.ErrHostClusterNameTaken)

	cluster := getHostCluster(t, hub)
	assert.False(t, cluster.IsHostCluster(), "the user's cluster must be left untouched")
}

func TestHostClusterNamespacePrefersDownwardAPIEnv(t *testing.T) {
	t.Setenv("POD_NAMESPACE", hostNamespace)

	assert.Equal(t, hostNamespace, operator.HostClusterNamespace())
}

func TestHostClusterNamespaceDefaultsOutsideCluster(t *testing.T) {
	// Empty env falls through to the ServiceAccount namespace file, which does not exist on a test
	// machine, leaving the "default" fallback.
	t.Setenv("POD_NAMESPACE", "")

	assert.Equal(t, "default", operator.HostClusterNamespace())
}

func TestNewHostStatusObserverReportsEndpointWhenNodesUnreachable(t *testing.T) {
	t.Parallel()

	// Port 1 on localhost refuses connections immediately, so the observer's node count fails while
	// the endpoint (known from the REST config alone) is still reported.
	observer := operator.NewHostStatusObserver(&rest.Config{
		Host:    "https://127.0.0.1:1",
		Timeout: time.Second,
	})

	observed, err := observer(context.Background(), nil, &v1alpha1.Cluster{})
	require.Error(t, err)
	assert.Equal(t, "https://127.0.0.1:1", observed.Endpoint)
	assert.False(t, observed.NodesObserved)
}

func TestCountReadyNodes(t *testing.T) {
	t.Parallel()

	clientset := kubefake.NewClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "ready"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			}},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "not-ready"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			}},
		},
	)

	ready, total, err := operator.CountReadyNodes(context.Background(), clientset)
	require.NoError(t, err)
	assert.Equal(t, int32(1), ready)
	assert.Equal(t, int32(2), total)
}
