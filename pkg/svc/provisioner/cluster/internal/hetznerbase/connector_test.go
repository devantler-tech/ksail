package hetznerbase_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

const (
	connectorTestCluster   = "test-cluster"
	connectorTestNamespace = "ksail-system"
	connectorTestPrefix    = "k3s-hetzner"
	connectorTestSecret    = connectorTestPrefix + "-" + connectorTestCluster + "-kubeconfig"
	connectorTestKey       = "kubeconfig.yaml"
)

// newConnectorBase builds a Base wired for the Connector paths only — the hub
// clientset, namespace, secret prefix, and default cluster name; the Hetzner
// infra seams stay nil because publish/read/delete never touch them.
func newConnectorBase(hub kubernetes.Interface) *hetznerbase.Base {
	return &hetznerbase.Base{
		ClusterName:           connectorTestCluster,
		Hub:                   hub,
		HubNamespace:          connectorTestNamespace,
		ConnectorSecretPrefix: connectorTestPrefix,
	}
}

func publishedSecret(data []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      connectorTestSecret,
			Namespace: connectorTestNamespace,
		},
		Data: map[string][]byte{connectorTestKey: data},
	}
}

func TestConnectorSecretName(t *testing.T) {
	t.Parallel()

	base := newConnectorBase(nil)

	assert.Equal(t, connectorTestSecret, base.ConnectorSecretNameForTest(connectorTestCluster))
}

func TestKubeconfig_NotReadyWhileSecretUnpublished(t *testing.T) {
	t.Parallel()

	base := newConnectorBase(k8sfake.NewClientset())

	_, err := base.Kubeconfig(context.Background(), connectorTestCluster)

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_NotReadyWhileSecretKeyEmpty(t *testing.T) {
	t.Parallel()

	base := newConnectorBase(k8sfake.NewClientset(publishedSecret(nil)))

	_, err := base.Kubeconfig(context.Background(), connectorTestCluster)

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_ReturnsPublishedSecret(t *testing.T) {
	t.Parallel()

	kubeconfig := []byte("apiVersion: v1\nkind: Config\n")
	base := newConnectorBase(k8sfake.NewClientset(publishedSecret(kubeconfig)))

	raw, err := base.Kubeconfig(context.Background(), connectorTestCluster)

	require.NoError(t, err)
	assert.Equal(t, kubeconfig, raw)
}

func TestKubeconfig_ResolvesNameFromConfigDefault(t *testing.T) {
	t.Parallel()

	kubeconfig := []byte("apiVersion: v1\nkind: Config\n")
	base := newConnectorBase(k8sfake.NewClientset(publishedSecret(kubeconfig)))

	raw, err := base.Kubeconfig(context.Background(), "")

	require.NoError(t, err)
	assert.Equal(t, kubeconfig, raw)
}

func TestKubeconfig_ErrorsWithoutHub(t *testing.T) {
	t.Parallel()

	base := newConnectorBase(nil)

	_, err := base.Kubeconfig(context.Background(), connectorTestCluster)

	require.ErrorIs(t, err, clustererr.ErrConfigNil)
}

func TestPublishConnectorKubeconfig_SkipsWithoutHub(t *testing.T) {
	t.Parallel()

	base := newConnectorBase(nil)

	err := base.PublishConnectorKubeconfigForTest(
		context.Background(), connectorTestCluster, []byte("ignored"),
	)

	require.NoError(t, err)
}

func TestPublishConnectorKubeconfig_PublishesSecret(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	base := newConnectorBase(clientset)
	kubeconfig := []byte("apiVersion: v1\nkind: Config\n")

	err := base.PublishConnectorKubeconfigForTest(
		context.Background(), connectorTestCluster, kubeconfig,
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets(connectorTestNamespace).
		Get(context.Background(), connectorTestSecret, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, kubeconfig, secret.Data[connectorTestKey])
	assert.Equal(t, "ksail", secret.Labels["ksail.io/managed-by"])
	assert.Equal(t, connectorTestCluster, secret.Labels["ksail.io/cluster"])
}

func TestPublishConnectorKubeconfig_IsIdempotent(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	base := newConnectorBase(clientset)

	err := base.PublishConnectorKubeconfigForTest(
		context.Background(), connectorTestCluster, []byte("first"),
	)
	require.NoError(t, err)

	err = base.PublishConnectorKubeconfigForTest(
		context.Background(), connectorTestCluster, []byte("second"),
	)
	require.NoError(t, err)

	raw, err := base.Kubeconfig(context.Background(), connectorTestCluster)
	require.NoError(t, err)
	assert.Equal(t, []byte("second"), raw)
}

func TestDeleteConnectorKubeconfig_RemovesPublishedSecret(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(publishedSecret([]byte("kubeconfig")))
	base := newConnectorBase(clientset)

	err := base.DeleteConnectorKubeconfigForTest(context.Background(), connectorTestCluster)
	require.NoError(t, err)

	_, err = base.Kubeconfig(context.Background(), connectorTestCluster)
	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestDeleteConnectorKubeconfig_ToleratesAbsentSecret(t *testing.T) {
	t.Parallel()

	base := newConnectorBase(k8sfake.NewClientset())

	err := base.DeleteConnectorKubeconfigForTest(context.Background(), connectorTestCluster)

	require.NoError(t, err)
}

func TestDeleteConnectorKubeconfig_SkipsWithoutHub(t *testing.T) {
	t.Parallel()

	base := newConnectorBase(nil)

	err := base.DeleteConnectorKubeconfigForTest(context.Background(), connectorTestCluster)

	require.NoError(t, err)
}
