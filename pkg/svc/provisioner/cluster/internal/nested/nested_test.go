package nested_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/nested"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestReadyTimeout_FallbackWhenUnset(t *testing.T) {
	t.Setenv(nested.ReadyTimeoutEnvVar, "")

	got := nested.ReadyTimeout(7 * time.Minute)
	assert.Equal(t, 7*time.Minute, got)
}

func TestReadyTimeout_OverrideHonored(t *testing.T) {
	t.Setenv(nested.ReadyTimeoutEnvVar, "15m")

	got := nested.ReadyTimeout(7 * time.Minute)
	assert.Equal(t, 15*time.Minute, got)
}

func TestDebugEnabled(t *testing.T) {
	t.Setenv(nested.DebugEnvVar, "")
	assert.False(t, nested.DebugEnabled())

	t.Setenv(nested.DebugEnvVar, "1")
	assert.True(t, nested.DebugEnabled())
}

func TestNamespaceExists(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "vcluster-present"},
	})

	exists, err := nested.NamespaceExists(context.Background(), clientset, "vcluster-present")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = nested.NamespaceExists(context.Background(), clientset, "vcluster-absent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestWaitForKubeconfigSecret_ReturnsData(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vc-demo", Namespace: "vcluster-demo"},
		Data:       map[string][]byte{"config": []byte("kubeconfig-bytes")},
	})

	data, err := nested.WaitForKubeconfigSecret(
		context.Background(), clientset, "vcluster-demo", "vc-demo", "config",
		time.Millisecond, time.Second,
	)
	require.NoError(t, err)
	assert.Equal(t, []byte("kubeconfig-bytes"), data)
}

func TestWaitForKubeconfigSecret_TimesOutWhenMissing(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()

	_, err := nested.WaitForKubeconfigSecret(
		context.Background(), clientset, "vcluster-demo", "vc-demo", "config",
		time.Millisecond, 20*time.Millisecond,
	)
	require.Error(t, err)
}

func TestSetDockerHost_SetsAndRestores(t *testing.T) {
	// Not parallel: mutates the process DOCKER_HOST env var.
	const sentinel = "tcp://example:1234"

	t.Setenv("DOCKER_HOST", sentinel)

	restore, err := nested.SetDockerHost(2375)
	require.NoError(t, err)
	assert.Equal(t, "tcp://127.0.0.1:2375", os.Getenv("DOCKER_HOST"))

	restore()
	assert.Equal(t, sentinel, os.Getenv("DOCKER_HOST"))
}

func TestFetchKubeconfigSecret_NotFoundIsNotReady(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()

	_, err := nested.FetchKubeconfigSecret(
		context.Background(), clientset, "k3k-demo", "k3k-demo-kubeconfig", "kubeconfig.yaml",
	)
	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestFetchKubeconfigSecret_EmptyKeyIsNotReady(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "k3k-demo-kubeconfig", Namespace: "k3k-demo"},
		Data:       map[string][]byte{"other": []byte("x")},
	})

	_, err := nested.FetchKubeconfigSecret(
		context.Background(), clientset, "k3k-demo", "k3k-demo-kubeconfig", "kubeconfig.yaml",
	)
	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestFetchKubeconfigSecret_ReturnsData(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vc-demo", Namespace: "vcluster-demo"},
		Data:       map[string][]byte{"config": []byte("kubeconfig-bytes")},
	})

	raw, err := nested.FetchKubeconfigSecret(
		context.Background(), clientset, "vcluster-demo", "vc-demo", "config",
	)
	require.NoError(t, err)
	assert.Equal(t, []byte("kubeconfig-bytes"), raw)
}
