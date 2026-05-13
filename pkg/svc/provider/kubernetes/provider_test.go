package kubernetes_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewProvider(t *testing.T) {
	t.Parallel()

	t.Run("valid_client", func(t *testing.T) {
		t.Parallel()

		prov, err := kubeprovider.NewProvider(fake.NewClientset(), v1alpha1.OptionsKubernetes{})
		require.NoError(t, err)
		assert.NotNil(t, prov)
	})

	t.Run("nil_client", func(t *testing.T) {
		t.Parallel()

		prov, err := kubeprovider.NewProvider(nil, v1alpha1.OptionsKubernetes{})
		require.ErrorIs(t, err, kubeprovider.ErrHostClientRequired)
		assert.Nil(t, prov)
	})
}

func TestProvider_IsAvailable(t *testing.T) {
	t.Parallel()

	prov, err := kubeprovider.NewProvider(fake.NewClientset(), v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)
	assert.True(t, prov.IsAvailable())
}

func TestProvider_ListNodes_Empty(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	nodes, err := prov.ListNodes(context.Background(), "test-cluster")
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestProvider_ListNodes_WithPods(t *testing.T) {
	t.Parallel()

	ns := kubeprovider.NamespaceName("test-cluster")

	client := fake.NewClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   ns,
				Labels: kubeprovider.CommonLabels("test-cluster"),
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-cp-0",
				Namespace: ns,
				Labels:    kubeprovider.NodeLabels("test-cluster", kubeprovider.RoleControlPlane, "K3s"),
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-worker-0",
				Namespace: ns,
				Labels:    kubeprovider.NodeLabels("test-cluster", kubeprovider.RoleWorker, "K3s"),
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	nodes, err := prov.ListNodes(context.Background(), "test-cluster")
	require.NoError(t, err)
	require.Len(t, nodes, 2)

	// Verify node info
	for _, node := range nodes {
		assert.Equal(t, "test-cluster", node.ClusterName)
		assert.Equal(t, "Running", node.State)
		assert.Contains(t, []string{"control-plane", "worker"}, node.Role)
	}
}

func TestProvider_ListAllClusters(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "ksail-cluster-a",
				Labels: kubeprovider.CommonLabels("cluster-a"),
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "ksail-cluster-b",
				Labels: kubeprovider.CommonLabels("cluster-b"),
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
	)

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	clusters, err := prov.ListAllClusters(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"cluster-a", "cluster-b"}, clusters)
}

func TestProvider_NodesExist(t *testing.T) {
	t.Parallel()

	ns := kubeprovider.NamespaceName("test-cluster")

	client := fake.NewClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-cp-0",
				Namespace: ns,
				Labels:    kubeprovider.NodeLabels("test-cluster", kubeprovider.RoleControlPlane, "K3s"),
			},
		},
	)

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	exists, err := prov.NodesExist(context.Background(), "test-cluster")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = prov.NodesExist(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestProvider_StartNodes_NoNodes(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	err = prov.StartNodes(context.Background(), "empty-cluster")
	require.ErrorIs(t, err, provider.ErrNoNodes)
}

func TestProvider_StopNodes_NoNodes(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	err = prov.StopNodes(context.Background(), "empty-cluster")
	require.ErrorIs(t, err, provider.ErrNoNodes)
}

func TestProvider_EnsureNamespace(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	err = prov.EnsureNamespace(context.Background(), "my-cluster")
	require.NoError(t, err)

	// Verify namespace was created with correct labels
	ns, err := client.CoreV1().Namespaces().Get(
		context.Background(),
		"ksail-my-cluster",
		metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "ksail", ns.Labels[kubeprovider.LabelManagedBy])
	assert.Equal(t, "my-cluster", ns.Labels[kubeprovider.LabelClusterName])
}

func TestProvider_GetClusterStatus(t *testing.T) {
	t.Parallel()

	ns := kubeprovider.NamespaceName("test-cluster")

	client := fake.NewClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-cp-0",
				Namespace: ns,
				Labels:    kubeprovider.NodeLabels("test-cluster", kubeprovider.RoleControlPlane, "K3s"),
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	status, err := prov.GetClusterStatus(context.Background(), "test-cluster")
	require.NoError(t, err)
	assert.True(t, status.Ready)
	assert.Equal(t, 1, status.NodesTotal)
	assert.Equal(t, 1, status.NodesReady)
	assert.Equal(t, "Running", status.Phase)
}

func TestNamespaceName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ksail-my-cluster", kubeprovider.NamespaceName("my-cluster"))
	assert.Equal(t, "ksail-test", kubeprovider.NamespaceName("test"))
}

func TestCommonLabels(t *testing.T) {
	t.Parallel()

	labels := kubeprovider.CommonLabels("my-cluster")
	assert.Equal(t, "ksail", labels[kubeprovider.LabelManagedBy])
	assert.Equal(t, "my-cluster", labels[kubeprovider.LabelClusterName])
	assert.Len(t, labels, 2)
}

func TestNodeLabels(t *testing.T) {
	t.Parallel()

	labels := kubeprovider.NodeLabels("my-cluster", kubeprovider.RoleControlPlane, "K3s")
	assert.Equal(t, "ksail", labels[kubeprovider.LabelManagedBy])
	assert.Equal(t, "my-cluster", labels[kubeprovider.LabelClusterName])
	assert.Equal(t, "control-plane", labels[kubeprovider.LabelNodeRole])
	assert.Equal(t, "K3s", labels[kubeprovider.LabelDistribution])
	assert.Len(t, labels, 4)
}
