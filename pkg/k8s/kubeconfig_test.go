package k8s_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

// TestCleanupKubeconfig_NonExistentFile tests cleanup when kubeconfig doesn't exist.
func TestCleanupKubeconfig_NonExistentFile(t *testing.T) {
	t.Parallel()

	err := k8s.CleanupKubeconfig(
		"/nonexistent/path/kubeconfig",
		"cluster",
		"context",
		"user",
		io.Discard,
	)

	require.NoError(t, err, "should succeed silently when file doesn't exist")
}

// TestCleanupKubeconfig_NoMatchingEntries tests cleanup when no entries match.
func TestCleanupKubeconfig_NoMatchingEntries(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	validKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://other.server:6443
  name: other-cluster
contexts:
- context:
    cluster: other-cluster
    user: other-user
  name: other-context
current-context: other-context
users:
- name: other-user
  user:
    token: fake-token
`

	err := os.WriteFile(kubeconfigPath, []byte(validKubeconfig), 0o600)
	require.NoError(t, err)

	// Try to cleanup non-existent entries
	err = k8s.CleanupKubeconfig(
		kubeconfigPath,
		"nonexistent-cluster",
		"nonexistent-context",
		"nonexistent-user",
		io.Discard,
	)

	require.NoError(t, err)

	// Verify original content is unchanged
	content, err := os.ReadFile(kubeconfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "other-cluster")
	assert.Contains(t, string(content), "other-context")
	assert.Contains(t, string(content), "other-user")
}

// TestCleanupKubeconfig_RemovesMatchingEntries tests that matching entries are removed.
func TestCleanupKubeconfig_RemovesMatchingEntries(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	validKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://target.server:6443
  name: target-cluster
- cluster:
    server: https://other.server:6443
  name: other-cluster
contexts:
- context:
    cluster: target-cluster
    user: target-user
  name: target-context
- context:
    cluster: other-cluster
    user: other-user
  name: other-context
current-context: other-context
users:
- name: target-user
  user:
    token: target-token
- name: other-user
  user:
    token: other-token
`

	err := os.WriteFile(kubeconfigPath, []byte(validKubeconfig), 0o600)
	require.NoError(t, err)

	// Cleanup target entries
	err = k8s.CleanupKubeconfig(
		kubeconfigPath,
		"target-cluster",
		"target-context",
		"target-user",
		io.Discard,
	)

	require.NoError(t, err)

	// Verify target entries are removed
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	require.NoError(t, err)

	_, hasTargetCluster := config.Clusters["target-cluster"]
	_, hasTargetContext := config.Contexts["target-context"]
	_, hasTargetUser := config.AuthInfos["target-user"]

	assert.False(t, hasTargetCluster, "target cluster should be removed")
	assert.False(t, hasTargetContext, "target context should be removed")
	assert.False(t, hasTargetUser, "target user should be removed")

	// Verify other entries remain
	_, hasOtherCluster := config.Clusters["other-cluster"]
	_, hasOtherContext := config.Contexts["other-context"]
	_, hasOtherUser := config.AuthInfos["other-user"]

	assert.True(t, hasOtherCluster, "other cluster should remain")
	assert.True(t, hasOtherContext, "other context should remain")
	assert.True(t, hasOtherUser, "other user should remain")
}

// TestCleanupKubeconfig_ClearsCurrentContext tests that current-context is cleared when matching.
func TestCleanupKubeconfig_ClearsCurrentContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	validKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://target.server:6443
  name: target-cluster
contexts:
- context:
    cluster: target-cluster
    user: target-user
  name: target-context
current-context: target-context
users:
- name: target-user
  user:
    token: target-token
`

	err := os.WriteFile(kubeconfigPath, []byte(validKubeconfig), 0o600)
	require.NoError(t, err)

	// Cleanup entries including current context
	err = k8s.CleanupKubeconfig(
		kubeconfigPath,
		"target-cluster",
		"target-context",
		"target-user",
		io.Discard,
	)

	require.NoError(t, err)

	// Verify current-context is cleared
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	require.NoError(t, err)

	assert.Empty(t, config.CurrentContext, "current-context should be cleared")
}

// TestCleanupKubeconfig_WritesLogMessage tests that log message is written.
func TestCleanupKubeconfig_WritesLogMessage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	validKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://target.server:6443
  name: target-cluster
contexts:
- context:
    cluster: target-cluster
    user: target-user
  name: target-context
current-context: target-context
users:
- name: target-user
  user:
    token: target-token
`

	err := os.WriteFile(kubeconfigPath, []byte(validKubeconfig), 0o600)
	require.NoError(t, err)

	// Capture log output
	var logBuffer bytes.Buffer

	err = k8s.CleanupKubeconfig(
		kubeconfigPath,
		"target-cluster",
		"target-context",
		"target-user",
		&logBuffer,
	)

	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(), "Cleaned up kubeconfig entries")
	assert.Contains(t, logBuffer.String(), "target-cluster")
}

// TestCleanupKubeconfig_InvalidYAML tests handling of invalid YAML content.
func TestCleanupKubeconfig_InvalidYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	invalidYAML := `this is not valid yaml {{{`

	err := os.WriteFile(kubeconfigPath, []byte(invalidYAML), 0o600)
	require.NoError(t, err)

	err = k8s.CleanupKubeconfig(
		kubeconfigPath,
		"cluster",
		"context",
		"user",
		io.Discard,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse kubeconfig")
}

// TestCleanupKubeconfig_PartialMatch tests cleanup when only some entries match.
func TestCleanupKubeconfig_PartialMatch(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	// Only has cluster and context, not the user
	validKubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://target.server:6443
  name: target-cluster
contexts:
- context:
    cluster: target-cluster
    user: different-user
  name: target-context
current-context: target-context
users:
- name: different-user
  user:
    token: token
`

	err := os.WriteFile(kubeconfigPath, []byte(validKubeconfig), 0o600)
	require.NoError(t, err)

	// Try to cleanup - user doesn't match, but cluster and context do
	err = k8s.CleanupKubeconfig(
		kubeconfigPath,
		"target-cluster",
		"target-context",
		"nonexistent-user",
		io.Discard,
	)

	require.NoError(t, err)

	// Verify matching entries are removed
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	require.NoError(t, err)

	_, hasTargetCluster := config.Clusters["target-cluster"]
	_, hasTargetContext := config.Contexts["target-context"]
	_, hasDifferentUser := config.AuthInfos["different-user"]

	assert.False(t, hasTargetCluster, "target cluster should be removed")
	assert.False(t, hasTargetContext, "target context should be removed")
	assert.True(t, hasDifferentUser, "different user should remain")
}
