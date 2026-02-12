package k8s_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDynamicClient_EmptyKubeconfig tests that empty kubeconfig path returns error.
func TestNewDynamicClient_EmptyKubeconfig(t *testing.T) {
	t.Parallel()

	client, err := k8s.NewDynamicClient("", "")

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to build rest config")
	assert.ErrorIs(t, err, k8s.ErrKubeconfigPathEmpty)
}

// TestNewDynamicClient_ValidKubeconfig tests successful creation of dynamic client.
func TestNewDynamicClient_ValidKubeconfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	err := os.WriteFile(kubeconfigPath, []byte(testKubeconfigYAML), 0o600)
	require.NoError(t, err)

	client, err := k8s.NewDynamicClient(kubeconfigPath, "")

	require.NoError(t, err)
	require.NotNil(t, client)
}

// TestNewDynamicClient_WithContext tests creation of dynamic client with explicit context.
func TestNewDynamicClient_WithContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	err := os.WriteFile(kubeconfigPath, []byte(testKubeconfigYAML), 0o600)
	require.NoError(t, err)

	client, err := k8s.NewDynamicClient(kubeconfigPath, "test-context")

	require.NoError(t, err)
	require.NotNil(t, client)
}

// TestNewDynamicClient_NonExistentPath tests handling of non-existent kubeconfig path.
func TestNewDynamicClient_NonExistentPath(t *testing.T) {
	t.Parallel()

	client, err := k8s.NewDynamicClient("/nonexistent/path/to/kubeconfig", "")

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to build rest config")
}
