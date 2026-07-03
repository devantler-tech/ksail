package talosprovisioner_test

import (
	"context"
	"path/filepath"
	"testing"

	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
)

// The nested Talos provisioner must satisfy the operator's Connector capability.
var _ clusterprovisioner.Connector = (*talosprovisioner.KubernetesProvisioner)(nil)

const talosConnectorTestCluster = "nested-talos"

// talosFetchedKubeconfig is a minimal valid kubeconfig as fetched from the Talos API during
// create — the server points at a DinD-internal loopback endpoint, unreachable from an
// operator pod, so the publish step must rewrite it.
const talosFetchedKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:56443
  name: nested-talos
contexts:
- context:
    cluster: nested-talos
    user: admin@nested-talos
  name: admin@nested-talos
current-context: admin@nested-talos
users:
- name: admin@nested-talos
  user: {}
`

func TestConnectionFor(t *testing.T) {
	t.Parallel()

	conn := talosprovisioner.ConnectionFor(talosConnectorTestCluster)

	assert.Equal(t, "ksail-nested-talos", conn.Namespace)
	assert.Equal(t, "talos-nested-talos-kubeconfig", conn.SecretName)
	assert.Equal(t, "https://apiserver.ksail-nested-talos:6443", conn.Endpoint)
}

func TestConnectionCertSANs(t *testing.T) {
	t.Parallel()

	conn := talosprovisioner.ConnectionFor(talosConnectorTestCluster)

	assert.Equal(
		t,
		[]string{"apiserver.ksail-nested-talos", "apiserver.ksail-nested-talos.svc"},
		conn.CertSANs(),
	)
}

func newTalosKubernetesProvisionerWithClientset(
	t *testing.T, clientset *k8sfake.Clientset,
) *talosprovisioner.KubernetesProvisioner {
	t.Helper()

	provisioner, err := talosprovisioner.NewKubernetesProvisioner(
		talosprovisioner.KubernetesProvisionerConfig{
			HostClientset:  clientset,
			ClusterName:    talosConnectorTestCluster,
			KubeconfigPath: filepath.Join(t.TempDir(), "kubeconfig"),
		},
	)
	require.NoError(t, err)

	return provisioner
}

func TestKubeconfig_NotReadyWhileSecretUnpublished(t *testing.T) {
	t.Parallel()

	provisioner := newTalosKubernetesProvisionerWithClientset(t, k8sfake.NewClientset())

	_, err := provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_NotReadyWhileSecretKeyEmpty(t *testing.T) {
	t.Parallel()

	conn := talosprovisioner.ConnectionFor(talosConnectorTestCluster)
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
	})
	provisioner := newTalosKubernetesProvisionerWithClientset(t, clientset)

	_, err := provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_ReturnsSecretAsPublished(t *testing.T) {
	t.Parallel()

	published := []byte("apiVersion: v1\nkind: Config\n")
	conn := talosprovisioner.ConnectionFor(talosConnectorTestCluster)
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": published},
	})
	provisioner := newTalosKubernetesProvisionerWithClientset(t, clientset)

	out, err := provisioner.Kubeconfig(context.Background(), "")

	require.NoError(t, err)
	assert.Equal(t, published, out)
}

func TestKubeconfig_ErrorsWhenClientsetNil(t *testing.T) {
	t.Parallel()

	provisioner, err := talosprovisioner.NewKubernetesProvisioner(
		talosprovisioner.KubernetesProvisionerConfig{
			ClusterName:    talosConnectorTestCluster,
			KubeconfigPath: filepath.Join(t.TempDir(), "kubeconfig"),
		},
	)
	require.NoError(t, err)

	_, err = provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrConfigNil)
}

func TestPublishConnectorKubeconfig_RewritesServerToInClusterEndpoint(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	provisioner := newTalosKubernetesProvisionerWithClientset(t, clientset)
	conn := talosprovisioner.ConnectionFor(talosConnectorTestCluster)

	err := provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), talosConnectorTestCluster, []byte(talosFetchedKubeconfig),
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().
		Secrets(conn.Namespace).
		Get(context.Background(), conn.SecretName, metav1.GetOptions{})
	require.NoError(t, err)

	config, err := clientcmd.Load(secret.Data["kubeconfig.yaml"])
	require.NoError(t, err)
	require.NotEmpty(t, config.Clusters)

	for _, cluster := range config.Clusters {
		assert.Equal(t, conn.Endpoint, cluster.Server)
	}
}

func TestPublishConnectorKubeconfig_UpdatesExistingSecret(t *testing.T) {
	t.Parallel()

	conn := talosprovisioner.ConnectionFor(talosConnectorTestCluster)
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": []byte("stale")},
	})
	provisioner := newTalosKubernetesProvisionerWithClientset(t, clientset)

	err := provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), talosConnectorTestCluster, []byte(talosFetchedKubeconfig),
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().
		Secrets(conn.Namespace).
		Get(context.Background(), conn.SecretName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.NotEqual(t, []byte("stale"), secret.Data["kubeconfig.yaml"])
}

func TestPublishConnectorKubeconfig_ErrorsWhenClientsetNil(t *testing.T) {
	t.Parallel()

	provisioner, err := talosprovisioner.NewKubernetesProvisioner(
		talosprovisioner.KubernetesProvisionerConfig{
			ClusterName:    talosConnectorTestCluster,
			KubeconfigPath: filepath.Join(t.TempDir(), "kubeconfig"),
		},
	)
	require.NoError(t, err)

	err = provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), talosConnectorTestCluster, []byte(talosFetchedKubeconfig),
	)

	require.ErrorIs(t, err, clustererr.ErrConfigNil)
}
