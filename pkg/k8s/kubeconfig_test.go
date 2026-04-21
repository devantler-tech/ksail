package k8s_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

// kubeconfigWithTargetAndOther is a kubeconfig with both target and other entries for cleanup tests.
const kubeconfigWithTargetAndOther = `apiVersion: v1
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
	//nolint:gosec // G304: Safe in test context with controlled paths
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

	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigWithTargetAndOther), 0o600)
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

	// Verify target entries are removed and other entries remain
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	require.NoError(t, err)

	_, hasTargetCluster := config.Clusters["target-cluster"]
	_, hasTargetContext := config.Contexts["target-context"]
	_, hasTargetUser := config.AuthInfos["target-user"]

	assert.False(t, hasTargetCluster, "target cluster should be removed")
	assert.False(t, hasTargetContext, "target context should be removed")
	assert.False(t, hasTargetUser, "target user should be removed")

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

// TestRenameKubeconfigContext tests context renaming in kubeconfig bytes.
//
//nolint:funlen // table-driven test with multiple test cases
func TestRenameKubeconfigContext(t *testing.T) {
	t.Parallel()

	omniKubeconfig := `apiVersion: v1
kind: Config
current-context: devantler-devantler-dev-ksail
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: devantler-devantler-dev-ksail
contexts:
- context:
    cluster: devantler-devantler-dev-ksail
    user: devantler-devantler-dev-ksail
  name: devantler-devantler-dev-ksail
users:
- name: devantler-devantler-dev-ksail
  user:
    token: test-token
`

	tests := []struct {
		name           string
		kubeconfig     string
		desiredContext string
		wantContext    string
		wantErr        bool
		errContains    string
	}{
		{
			name:           "renames Omni SA context",
			kubeconfig:     omniKubeconfig,
			desiredContext: "admin@devantler-dev",
			wantContext:    "admin@devantler-dev",
		},
		{
			name:           "no-op when already correct",
			kubeconfig:     omniKubeconfig,
			desiredContext: "devantler-devantler-dev-ksail",
			wantContext:    "devantler-devantler-dev-ksail",
		},
		{
			name:           "error on invalid kubeconfig",
			kubeconfig:     "not-valid-yaml: {{{",
			desiredContext: "some-context",
			wantErr:        true,
		},
		{
			name: "error when no current context and multiple entries",
			kubeconfig: `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://a:6443
  name: ctx-a
- cluster:
    server: https://b:6443
  name: ctx-b
contexts:
- context:
    cluster: ctx-a
    user: user-a
  name: ctx-a
- context:
    cluster: ctx-b
    user: user-b
  name: ctx-b
users:
- name: user-a
  user:
    token: a
- name: user-b
  user:
    token: b
`,
			desiredContext: "admin@test",
			wantErr:        true,
			errContains:    "no current context",
		},
		{
			name: "picks sole context when current context is empty",
			kubeconfig: `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: only-ctx
contexts:
- context:
    cluster: only-ctx
    user: only-ctx
  name: only-ctx
users:
- name: only-ctx
  user:
    token: t
`,
			desiredContext: "admin@my-cluster",
			wantContext:    "admin@my-cluster",
		},
		{
			name: "error on empty kubeconfig with no contexts",
			kubeconfig: `apiVersion: v1
kind: Config
contexts: []
clusters: []
users: []
`,
			desiredContext: "admin@test",
			wantErr:        true,
			errContains:    "no contexts in kubeconfig",
		},
		{
			name: "returns error on context name collision",
			kubeconfig: `apiVersion: v1
kind: Config
current-context: old-ctx
clusters:
- cluster:
    server: https://old:6443
  name: old-ctx
- cluster:
    server: https://new:6443
  name: new-ctx
contexts:
- context:
    cluster: old-ctx
    user: old-ctx
  name: old-ctx
- context:
    cluster: new-ctx
    user: new-user
  name: new-ctx
users:
- name: old-ctx
  user:
    token: old-token
- name: new-user
  user:
    token: new-token
`,
			desiredContext: "new-ctx",
			wantErr:        true,
			errContains:    "context name collision",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := k8s.RenameKubeconfigContext(
				[]byte(testCase.kubeconfig), testCase.desiredContext,
			)

			if testCase.wantErr {
				require.Error(t, err)

				if testCase.errContains != "" {
					assert.Contains(t, err.Error(), testCase.errContains)
				}

				return
			}

			require.NoError(t, err)

			config, parseErr := clientcmd.Load(result)
			require.NoError(t, parseErr)
			assert.Equal(t, testCase.wantContext, config.CurrentContext)
		})
	}
}
