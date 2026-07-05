package kindprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
)

// The nested Kind provisioner must satisfy the operator's Connector capability so InstallComponents
// installs components into the child cluster instead of skipping it.
var _ clusterprovisioner.Connector = (*kindprovisioner.KubernetesProvisioner)(nil)

const kindConnectorTestCluster = "nested-kind"

// kindHostKubeconfig is a shared host kubeconfig carrying both the host context and the nested
// "kind-<name>" context the Kind SDK writes on create. The publish step must minify it down to the
// nested context so the operator's current-context points at the child, not the host.
const kindHostKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://host.example:6443
  name: host
- cluster:
    server: https://10.0.0.5:31234
  name: kind-nested-kind
contexts:
- context:
    cluster: host
    user: host
  name: host
- context:
    cluster: kind-nested-kind
    user: kind-nested-kind
  name: kind-nested-kind
current-context: host
users:
- name: host
  user: {}
- name: kind-nested-kind
  user:
    token: nested-token
`

func TestConnectionFor(t *testing.T) {
	t.Parallel()

	conn := kindprovisioner.ConnectionFor(kindConnectorTestCluster)

	assert.Equal(t, "ksail-nested-kind", conn.Namespace)
	assert.Equal(t, "kind-nested-kind-kubeconfig", conn.SecretName)
	assert.Equal(t, "kind-nested-kind", conn.ContextName)
}

func TestKubeconfig_NotReadyWhileSecretUnpublished(t *testing.T) {
	t.Parallel()

	provisioner := kindprovisioner.NewKubernetesProvisionerForConnectorTest(
		k8sfake.NewClientset(), kindConnectorTestCluster, "",
	)

	_, err := provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_NotReadyWhileSecretKeyEmpty(t *testing.T) {
	t.Parallel()

	conn := kindprovisioner.ConnectionFor(kindConnectorTestCluster)
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": {}},
	})
	provisioner := kindprovisioner.NewKubernetesProvisionerForConnectorTest(
		clientset, kindConnectorTestCluster, "",
	)

	_, err := provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_ReturnsPublishedSecret(t *testing.T) {
	t.Parallel()

	conn := kindprovisioner.ConnectionFor(kindConnectorTestCluster)
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": []byte(kindHostKubeconfig)},
	})
	provisioner := kindprovisioner.NewKubernetesProvisionerForConnectorTest(
		clientset, kindConnectorTestCluster, "",
	)

	out, err := provisioner.Kubeconfig(context.Background(), "")

	require.NoError(t, err)
	assert.Equal(t, []byte(kindHostKubeconfig), out)
}

func TestKubeconfig_ResolvesNameFromArgument(t *testing.T) {
	t.Parallel()

	conn := kindprovisioner.ConnectionFor(kindConnectorTestCluster)
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": []byte("kubeconfig-bytes")},
	})
	// Empty kindConfig name — the Kubeconfig(name) argument must resolve the cluster.
	provisioner := kindprovisioner.NewKubernetesProvisionerForConnectorTest(clientset, "", "")

	out, err := provisioner.Kubeconfig(context.Background(), kindConnectorTestCluster)

	require.NoError(t, err)
	assert.Equal(t, []byte("kubeconfig-bytes"), out)
}

func TestPublishConnectorKubeconfig_PublishesMinifiedSecret(t *testing.T) {
	t.Parallel()

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kindHostKubeconfig), 0o600))

	clientset := k8sfake.NewClientset()
	provisioner := kindprovisioner.NewKubernetesProvisionerForConnectorTest(
		clientset, kindConnectorTestCluster, kubeconfigPath,
	)

	err := provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), kindConnectorTestCluster,
	)
	require.NoError(t, err)

	conn := kindprovisioner.ConnectionFor(kindConnectorTestCluster)
	secret, err := clientset.CoreV1().
		Secrets(conn.Namespace).
		Get(context.Background(), conn.SecretName, metav1.GetOptions{})
	require.NoError(t, err)

	// The published kubeconfig is minified to the single nested context, with the host context
	// dropped and current-context pointing at the child cluster.
	published, err := clientcmd.Load(secret.Data["kubeconfig.yaml"])
	require.NoError(t, err)
	assert.Equal(t, "kind-nested-kind", published.CurrentContext)
	assert.Contains(t, published.Clusters, "kind-nested-kind")
	assert.NotContains(t, published.Clusters, "host")
	assert.Contains(t, published.Contexts, "kind-nested-kind")
	assert.NotContains(t, published.Contexts, "host")

	// The read side serves exactly what was published.
	out, err := provisioner.Kubeconfig(context.Background(), kindConnectorTestCluster)
	require.NoError(t, err)
	assert.Equal(t, secret.Data["kubeconfig.yaml"], out)
}

func TestPublishConnectorKubeconfig_IsIdempotent(t *testing.T) {
	t.Parallel()

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kindHostKubeconfig), 0o600))

	clientset := k8sfake.NewClientset()
	provisioner := kindprovisioner.NewKubernetesProvisionerForConnectorTest(
		clientset, kindConnectorTestCluster, kubeconfigPath,
	)

	require.NoError(t, provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), kindConnectorTestCluster,
	))
	require.NoError(t, provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), kindConnectorTestCluster,
	))

	out, err := provisioner.Kubeconfig(context.Background(), kindConnectorTestCluster)
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestPublishConnectorKubeconfig_ErrorsWhenContextMissing(t *testing.T) {
	t.Parallel()

	// A kubeconfig without the expected "kind-<name>" context — extraction must fail loudly.
	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://host.example:6443
  name: host
contexts:
- context:
    cluster: host
    user: host
  name: host
current-context: host
users:
- name: host
  user: {}
`), 0o600))

	provisioner := kindprovisioner.NewKubernetesProvisionerForConnectorTest(
		k8sfake.NewClientset(), kindConnectorTestCluster, kubeconfigPath,
	)

	err := provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), kindConnectorTestCluster,
	)

	require.ErrorIs(t, err, clustererr.ErrKubeconfigContextMissing)
}
