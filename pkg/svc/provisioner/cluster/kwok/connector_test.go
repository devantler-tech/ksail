package kwokprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	kwokprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kwok"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
)

// The nested KWOK provisioner must satisfy the operator's Connector capability so InstallComponents
// installs components into the child cluster instead of skipping it.
var _ clusterprovisioner.Connector = (*kwokprovisioner.KubernetesProvisioner)(nil)

const kwokConnectorTestCluster = "nested-kwok"

// kwokHostKubeconfig is a shared host kubeconfig carrying both the host context and the nested
// "kwok-<name>" context kwokctl writes on create. The publish step must minify it down to the
// nested context so the operator's current-context points at the child, not the host.
const kwokHostKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://host.example:6443
  name: host
- cluster:
    server: https://10.0.0.5:31234
  name: kwok-nested-kwok
contexts:
- context:
    cluster: host
    user: host
  name: host
- context:
    cluster: kwok-nested-kwok
    user: kwok-nested-kwok
  name: kwok-nested-kwok
current-context: host
users:
- name: host
  user: {}
- name: kwok-nested-kwok
  user:
    token: nested-token
`

func TestConnectionFor(t *testing.T) {
	t.Parallel()

	conn := kwokprovisioner.ConnectionFor(kwokConnectorTestCluster)

	assert.Equal(t, "ksail-nested-kwok", conn.Namespace)
	assert.Equal(t, "kwok-nested-kwok-kubeconfig", conn.SecretName)
	assert.Equal(t, "kwok-nested-kwok", conn.ContextName)
}

func TestKubeconfig_NotReadyWhileSecretUnpublished(t *testing.T) {
	t.Parallel()

	provisioner := kwokprovisioner.NewKubernetesProvisionerForConnectorTest(
		k8sfake.NewClientset(), kwokConnectorTestCluster, "",
	)

	_, err := provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_NotReadyWhileSecretKeyEmpty(t *testing.T) {
	t.Parallel()

	conn := kwokprovisioner.ConnectionFor(kwokConnectorTestCluster)
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": {}},
	})
	provisioner := kwokprovisioner.NewKubernetesProvisionerForConnectorTest(
		clientset, kwokConnectorTestCluster, "",
	)

	_, err := provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_ReturnsPublishedSecret(t *testing.T) {
	t.Parallel()

	conn := kwokprovisioner.ConnectionFor(kwokConnectorTestCluster)
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": []byte(kwokHostKubeconfig)},
	})
	provisioner := kwokprovisioner.NewKubernetesProvisionerForConnectorTest(
		clientset, kwokConnectorTestCluster, "",
	)

	out, err := provisioner.Kubeconfig(context.Background(), "")

	require.NoError(t, err)
	assert.Equal(t, []byte(kwokHostKubeconfig), out)
}

func TestKubeconfig_ResolvesNameFromArgument(t *testing.T) {
	t.Parallel()

	conn := kwokprovisioner.ConnectionFor(kwokConnectorTestCluster)
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": []byte("kubeconfig-bytes")},
	})
	// Empty provisioner name — the Kubeconfig(name) argument must resolve the cluster.
	provisioner := kwokprovisioner.NewKubernetesProvisionerForConnectorTest(clientset, "", "")

	out, err := provisioner.Kubeconfig(context.Background(), kwokConnectorTestCluster)

	require.NoError(t, err)
	assert.Equal(t, []byte("kubeconfig-bytes"), out)
}

func TestPublishConnectorKubeconfig_PublishesMinifiedSecret(t *testing.T) {
	t.Parallel()

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kwokHostKubeconfig), 0o600))

	clientset := k8sfake.NewClientset()
	provisioner := kwokprovisioner.NewKubernetesProvisionerForConnectorTest(
		clientset, kwokConnectorTestCluster, kubeconfigPath,
	)

	err := provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), kwokConnectorTestCluster,
	)
	require.NoError(t, err)

	conn := kwokprovisioner.ConnectionFor(kwokConnectorTestCluster)
	secret, err := clientset.CoreV1().
		Secrets(conn.Namespace).
		Get(context.Background(), conn.SecretName, metav1.GetOptions{})
	require.NoError(t, err)

	// The published kubeconfig is minified to the single nested context, with the host context
	// dropped and current-context pointing at the child cluster.
	published, err := clientcmd.Load(secret.Data["kubeconfig.yaml"])
	require.NoError(t, err)
	assert.Equal(t, "kwok-nested-kwok", published.CurrentContext)
	assert.Contains(t, published.Clusters, "kwok-nested-kwok")
	assert.NotContains(t, published.Clusters, "host")
	assert.Contains(t, published.Contexts, "kwok-nested-kwok")
	assert.NotContains(t, published.Contexts, "host")

	// The read side serves exactly what was published.
	out, err := provisioner.Kubeconfig(context.Background(), kwokConnectorTestCluster)
	require.NoError(t, err)
	assert.Equal(t, secret.Data["kubeconfig.yaml"], out)
}

func TestPublishConnectorKubeconfig_IsIdempotent(t *testing.T) {
	t.Parallel()

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kwokHostKubeconfig), 0o600))

	clientset := k8sfake.NewClientset()
	provisioner := kwokprovisioner.NewKubernetesProvisionerForConnectorTest(
		clientset, kwokConnectorTestCluster, kubeconfigPath,
	)

	require.NoError(t, provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), kwokConnectorTestCluster,
	))
	require.NoError(t, provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), kwokConnectorTestCluster,
	))

	out, err := provisioner.Kubeconfig(context.Background(), kwokConnectorTestCluster)
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestPublishConnectorKubeconfig_ErrorsWhenContextMissing(t *testing.T) {
	t.Parallel()

	// A kubeconfig without the expected "kwok-<name>" context — extraction must fail loudly.
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

	provisioner := kwokprovisioner.NewKubernetesProvisionerForConnectorTest(
		k8sfake.NewClientset(), kwokConnectorTestCluster, kubeconfigPath,
	)

	err := provisioner.PublishConnectorKubeconfigForTest(
		context.Background(), kwokConnectorTestCluster,
	)

	require.ErrorIs(t, err, clustererr.ErrKubeconfigContextMissing)
}
