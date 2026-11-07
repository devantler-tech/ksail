package operator_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// minimalKubeconfig is a parseable kubeconfig pointing at an unreachable server, so countNodes gets
// past parsing and fails at the connection (exercising the best-effort error path).
const minimalKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: https://127.0.0.1:1
    insecure-skip-tls-verify: true
contexts:
- name: c
  context:
    cluster: c
    user: u
current-context: c
users:
- name: u
  user:
    token: t
`

func statusReader(t *testing.T, objects ...*corev1.Secret) *fake.ClientBuilder {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, secret := range objects {
		builder = builder.WithObjects(secret)
	}

	return builder
}

func clusterNamed(name, namespace string) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		Name: name, Namespace: namespace,
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionVCluster},
		},
	}
}

func TestObserveVClusterStatus_SecretNotReadyYet(t *testing.T) {
	t.Parallel()

	reader := statusReader(t).Build()

	observed, err := operator.ObserveVClusterStatus(
		context.Background(),
		reader,
		clusterNamed("myc", "default"),
	)

	// No kubeconfig Secret yet: nothing to report, and this is not an error.
	require.NoError(t, err)
	assert.Empty(t, observed.Endpoint)
	assert.Nil(t, observed.KubeconfigSecret)
	assert.False(t, observed.NodesObserved)
}

func TestObserveVClusterStatus_ReportsEndpointEvenWhenNodeCountFails(t *testing.T) {
	t.Parallel()

	secret := &corev1.Secret{
		Name:      "vc-default-myc",
		Namespace: "vcluster-default-myc",
		Data:      map[string][]byte{"config": []byte(minimalKubeconfig)},
	}

	reader := statusReader(t, secret).Build()

	observed, err := operator.ObserveVClusterStatus(
		context.Background(),
		reader,
		clusterNamed("myc", "default"),
	)

	// The child API server is unreachable, so node counting fails — but the endpoint and kubeconfig
	// reference are still derived and returned (best-effort: partial results are useful).
	require.Error(t, err)
	assert.Equal(t, "https://default-myc.vcluster-default-myc.svc:443", observed.Endpoint)
	require.NotNil(t, observed.KubeconfigSecret)
	assert.Equal(t, "vc-default-myc", observed.KubeconfigSecret.Name)
	assert.Equal(t, "vcluster-default-myc", observed.KubeconfigSecret.Namespace)
	assert.False(t, observed.NodesObserved)
}

func TestObserveVClusterStatus_SkipsNonVCluster(t *testing.T) {
	t.Parallel()

	reader := statusReader(t).Build()

	cluster := clusterNamed("myc", "default")
	cluster.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla

	observed, err := operator.ObserveVClusterStatus(context.Background(), reader, cluster)

	// Endpoint/node observation is vcluster-specific; other distributions report nothing.
	require.NoError(t, err)
	assert.Empty(t, observed.Endpoint)
	assert.Nil(t, observed.KubeconfigSecret)
}

func TestIsNodeReady(t *testing.T) {
	t.Parallel()

	ready := &corev1.Node{Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
	}}}
	notReady := &corev1.Node{Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
	}}}
	noCondition := &corev1.Node{}

	assert.True(t, operator.IsNodeReady(ready))
	assert.False(t, operator.IsNodeReady(notReady))
	assert.False(t, operator.IsNodeReady(noCondition))
}

func TestObserveHostNodesReportsCountsAndIdentity(t *testing.T) {
	t.Parallel()

	ready := identityNode{osImage: "Talos (v1.13.12)", providerID: "hcloud://101", ready: true}
	notReady := identityNode{osImage: "Talos (v1.13.12)", providerID: "hcloud://102"}
	nodes := buildNodes(ready, notReady)
	clientset := kubefake.NewClientset(&nodes[0], &nodes[1])

	observed, err := operator.ObserveHostNodes(context.Background(), clientset)
	require.NoError(t, err)
	assert.True(t, observed.NodesObserved)
	assert.Equal(t, int32(1), observed.NodesReady)
	assert.Equal(t, int32(2), observed.NodesTotal)
	assert.Equal(t, v1alpha1.DistributionTalos, observed.Distribution)
	assert.Equal(t, v1alpha1.ProviderHetzner, observed.Provider)
}

func TestObserveHostNodesWithoutMarkersReportsNoIdentity(t *testing.T) {
	t.Parallel()

	nodes := buildNodes(
		identityNode{osImage: "Ubuntu 24.04", kubeletVersion: "v1.36.4", ready: true},
	)
	clientset := kubefake.NewClientset(&nodes[0])

	observed, err := operator.ObserveHostNodes(context.Background(), clientset)
	require.NoError(t, err)
	assert.True(t, observed.NodesObserved)
	assert.Empty(t, observed.Distribution)
	assert.Empty(t, observed.Provider)
}
