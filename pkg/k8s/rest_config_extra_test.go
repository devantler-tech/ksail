package k8s_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultKubeconfigPath tests that DefaultKubeconfigPath returns a path
// under the user's home directory.
func TestDefaultKubeconfigPath(t *testing.T) {
	t.Parallel()

	path := k8s.DefaultKubeconfigPath()

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	expected := filepath.Join(homeDir, ".kube", "config")
	assert.Equal(t, expected, path)
}

// TestNewClientset_WithContext tests creating a clientset with a specific context.
func TestNewClientset_WithContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	err := os.WriteFile(kubeconfigPath, []byte(testKubeconfigYAML), 0o600)
	require.NoError(t, err)

	clientset, err := k8s.NewClientset(kubeconfigPath, "test-context")

	require.NoError(t, err)
	require.NotNil(t, clientset)
}

// TestNewClientset_NonExistentPath tests handling of non-existent kubeconfig path.
func TestNewClientset_NonExistentPath(t *testing.T) {
	t.Parallel()

	clientset, err := k8s.NewClientset("/nonexistent/path/to/kubeconfig", "")

	require.Error(t, err)
	assert.Nil(t, clientset)
	assert.Contains(t, err.Error(), "failed to build rest config")
}

// TestErrKubeconfigNoCurrentContext_ErrorMessage verifies the error message.
func TestErrKubeconfigNoCurrentContext_ErrorMessage(t *testing.T) {
	t.Parallel()

	require.Error(t, k8s.ErrKubeconfigNoCurrentContext)
	assert.Equal(t, "kubeconfig has no current context", k8s.ErrKubeconfigNoCurrentContext.Error())
}

// TestErrKubeconfigContextNotFound_ErrorMessage verifies the error message.
func TestErrKubeconfigContextNotFound_ErrorMessage(t *testing.T) {
	t.Parallel()

	require.Error(t, k8s.ErrKubeconfigContextNotFound)
	assert.Equal(t, "kubeconfig context not found", k8s.ErrKubeconfigContextNotFound.Error())
}

// TestErrKubeconfigContextCollision_ErrorMessage verifies the error message.
func TestErrKubeconfigContextCollision_ErrorMessage(t *testing.T) {
	t.Parallel()

	require.Error(t, k8s.ErrKubeconfigContextCollision)
	assert.Equal(t, "kubeconfig context name collision", k8s.ErrKubeconfigContextCollision.Error())
}
