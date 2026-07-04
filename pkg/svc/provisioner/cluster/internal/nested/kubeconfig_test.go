package nested_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/nested"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
)

const twoContextKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://host.example:6443
  name: host
- cluster:
    server: https://10.0.0.5:31234
  name: nested
contexts:
- context:
    cluster: host
    user: host
  name: host
- context:
    cluster: nested
    user: nested
  name: nested
current-context: host
users:
- name: host
  user: {}
- name: nested
  user:
    token: nested-token
`

func writeKubeconfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	return path
}

func TestExtractContextKubeconfig_MinifiesToSingleContext(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoContextKubeconfig)

	raw, err := nested.ExtractContextKubeconfig(path, "nested")
	require.NoError(t, err)

	config, err := clientcmd.Load(raw)
	require.NoError(t, err)

	assert.Equal(t, "nested", config.CurrentContext)
	assert.Contains(t, config.Contexts, "nested")
	assert.NotContains(t, config.Contexts, "host")
	assert.Contains(t, config.Clusters, "nested")
	assert.NotContains(t, config.Clusters, "host")
	assert.Contains(t, config.AuthInfos, "nested")
	assert.NotContains(t, config.AuthInfos, "host")
}

func TestExtractContextKubeconfig_MissingContext(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoContextKubeconfig)

	_, err := nested.ExtractContextKubeconfig(path, "absent")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigContextMissing)
}

func TestExtractContextKubeconfig_MissingCluster(t *testing.T) {
	t.Parallel()

	// The context references a cluster that is not defined.
	path := writeKubeconfig(t, `apiVersion: v1
kind: Config
contexts:
- context:
    cluster: gone
    user: nested
  name: nested
current-context: nested
users:
- name: nested
  user: {}
`)

	_, err := nested.ExtractContextKubeconfig(path, "nested")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigContextMissing)
}

func TestExtractContextKubeconfig_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := nested.ExtractContextKubeconfig(
		filepath.Join(t.TempDir(), "does-not-exist"), "nested",
	)

	require.Error(t, err)
}

func TestPublishKubeconfigSecret_CreatesSecret(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	labels := map[string]string{"app": "ksail"}

	err := nested.PublishKubeconfigSecret(
		context.Background(), clientset,
		"ksail-x", "kind-x-kubeconfig", "kubeconfig.yaml",
		[]byte("data"), labels,
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().
		Secrets("ksail-x").
		Get(context.Background(), "kind-x-kubeconfig", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), secret.Data["kubeconfig.yaml"])
	assert.Equal(t, labels, secret.Labels)
}

func TestPublishKubeconfigSecret_UpdatesInPlace(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()

	require.NoError(t, nested.PublishKubeconfigSecret(
		context.Background(), clientset,
		"ksail-x", "kind-x-kubeconfig", "kubeconfig.yaml", []byte("v1"), nil,
	))
	require.NoError(t, nested.PublishKubeconfigSecret(
		context.Background(), clientset,
		"ksail-x", "kind-x-kubeconfig", "kubeconfig.yaml", []byte("v2"), nil,
	))

	secret, err := clientset.CoreV1().
		Secrets("ksail-x").
		Get(context.Background(), "kind-x-kubeconfig", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), secret.Data["kubeconfig.yaml"])
}

func TestPublishKubeconfigSecret_NilClientset(t *testing.T) {
	t.Parallel()

	err := nested.PublishKubeconfigSecret(
		context.Background(), nil,
		"ksail-x", "kind-x-kubeconfig", "kubeconfig.yaml", []byte("data"), nil,
	)

	require.ErrorIs(t, err, clustererr.ErrKubeconfigPublishInvalid)
}

func TestPublishKubeconfigSecret_EmptyData(t *testing.T) {
	t.Parallel()

	err := nested.PublishKubeconfigSecret(
		context.Background(), k8sfake.NewClientset(),
		"ksail-x", "kind-x-kubeconfig", "kubeconfig.yaml", nil, nil,
	)

	require.ErrorIs(t, err, clustererr.ErrKubeconfigPublishInvalid)
}
