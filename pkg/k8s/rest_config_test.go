package k8s_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildRESTConfig_EmptyKubeconfig tests that empty kubeconfig path returns ErrKubeconfigPathEmpty.
func TestBuildRESTConfig_EmptyKubeconfig(t *testing.T) {
	t.Parallel()

	config, err := k8s.BuildRESTConfig("", "")

	require.Error(t, err)
	assert.Nil(t, config)
	assert.ErrorIs(t, err, k8s.ErrKubeconfigPathEmpty)
}

// TestBuildRESTConfig_NonExistentPath tests handling of non-existent kubeconfig path.
func TestBuildRESTConfig_NonExistentPath(t *testing.T) {
	t.Parallel()

	config, err := k8s.BuildRESTConfig("/nonexistent/path/to/kubeconfig", "")

	require.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to load kubeconfig")
}

// TestBuildRESTConfig_InvalidContent tests handling of invalid kubeconfig content.
func TestBuildRESTConfig_InvalidContent(t *testing.T) {
	t.Parallel()

	// Create a temporary file with invalid kubeconfig content
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "invalid-kubeconfig")

	err := os.WriteFile(kubeconfigPath, []byte("this is not valid yaml {{{"), 0o600)
	require.NoError(t, err)

	config, err := k8s.BuildRESTConfig(kubeconfigPath, "")

	require.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to load kubeconfig")
}

// TestBuildRESTConfig_ValidKubeconfig tests successful parsing of valid kubeconfig.
func TestBuildRESTConfig_ValidKubeconfig(t *testing.T) {
	t.Parallel()

	// Create a temporary file with valid kubeconfig content
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	validKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: fake-token
`

	err := os.WriteFile(kubeconfigPath, []byte(validKubeconfig), 0o600)
	require.NoError(t, err)

	config, err := k8s.BuildRESTConfig(kubeconfigPath, "")

	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Equal(t, "https://127.0.0.1:6443", config.Host)
}

// TestBuildRESTConfig_WithContext tests using a specific context from kubeconfig.
func TestBuildRESTConfig_WithContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	// Kubeconfig with multiple contexts
	validKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://default.server:6443
  name: default-cluster
- cluster:
    server: https://custom.server:6443
  name: custom-cluster
contexts:
- context:
    cluster: default-cluster
    user: default-user
  name: default-context
- context:
    cluster: custom-cluster
    user: custom-user
  name: custom-context
current-context: default-context
users:
- name: default-user
  user:
    token: default-token
- name: custom-user
  user:
    token: custom-token
`

	err := os.WriteFile(kubeconfigPath, []byte(validKubeconfig), 0o600)
	require.NoError(t, err)

	// Test with explicit context override
	config, err := k8s.BuildRESTConfig(kubeconfigPath, "custom-context")

	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Equal(t, "https://custom.server:6443", config.Host)
}

// TestBuildRESTConfig_NonExistentContext tests handling of non-existent context.
func TestBuildRESTConfig_NonExistentContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	validKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: fake-token
`

	err := os.WriteFile(kubeconfigPath, []byte(validKubeconfig), 0o600)
	require.NoError(t, err)

	config, err := k8s.BuildRESTConfig(kubeconfigPath, "nonexistent-context")

	require.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to load kubeconfig")
}

// TestNewClientset_EmptyKubeconfig tests that empty kubeconfig path returns error.
func TestNewClientset_EmptyKubeconfig(t *testing.T) {
	t.Parallel()

	clientset, err := k8s.NewClientset("", "")

	require.Error(t, err)
	assert.Nil(t, clientset)
	assert.Contains(t, err.Error(), "failed to build rest config")
	assert.ErrorIs(t, err, k8s.ErrKubeconfigPathEmpty)
}

// TestNewClientset_ValidKubeconfig tests successful creation of clientset.
func TestNewClientset_ValidKubeconfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	validKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: fake-token
`

	err := os.WriteFile(kubeconfigPath, []byte(validKubeconfig), 0o600)
	require.NoError(t, err)

	clientset, err := k8s.NewClientset(kubeconfigPath, "")

	require.NoError(t, err)
	require.NotNil(t, clientset)
}

// TestErrKubeconfigPathEmpty_ErrorMessage tests the error message content.
func TestErrKubeconfigPathEmpty_ErrorMessage(t *testing.T) {
	t.Parallel()

	assert.Contains(t, k8s.ErrKubeconfigPathEmpty.Error(), "kubeconfig path is empty")
}

// TestErrTimeoutExceeded_ErrorMessage tests the timeout error message content.
func TestErrTimeoutExceeded_ErrorMessage(t *testing.T) {
	t.Parallel()

	assert.Contains(t, k8s.ErrTimeoutExceeded.Error(), "timeout exceeded")
}
