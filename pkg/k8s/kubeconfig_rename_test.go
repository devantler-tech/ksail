package k8s_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

// TestRenameKubeconfigContext_EmptyDesiredContext verifies that an empty desired
// context returns the kubeconfig unchanged.
func TestRenameKubeconfigContext_EmptyDesiredContext(t *testing.T) {
	t.Parallel()

	input := []byte(`apiVersion: v1
kind: Config
current-context: my-ctx
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: my-ctx
contexts:
- context:
    cluster: my-ctx
    user: my-ctx
  name: my-ctx
users:
- name: my-ctx
  user:
    token: tok
`)

	result, err := k8s.RenameKubeconfigContext(input, "")

	require.NoError(t, err)
	// Should return unchanged
	config, parseErr := clientcmd.Load(result)
	require.NoError(t, parseErr)
	assert.Equal(t, "my-ctx", config.CurrentContext)
}

// TestRenameKubeconfigContext_ClusterNameDoesNotMatchOldContext verifies that
// cluster entries are not renamed when their name doesn't match the old context name.
func TestRenameKubeconfigContext_ClusterNameDoesNotMatchOldContext(t *testing.T) {
	t.Parallel()

	input := []byte(`apiVersion: v1
kind: Config
current-context: old-ctx
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: different-cluster
contexts:
- context:
    cluster: different-cluster
    user: old-ctx
  name: old-ctx
users:
- name: old-ctx
  user:
    token: tok
`)

	result, err := k8s.RenameKubeconfigContext(input, "new-ctx")

	require.NoError(t, err)

	config, parseErr := clientcmd.Load(result)
	require.NoError(t, parseErr)
	assert.Equal(t, "new-ctx", config.CurrentContext)

	// Cluster should NOT be renamed since its name didn't match old context
	_, hasOldCluster := config.Clusters["different-cluster"]
	assert.True(t, hasOldCluster, "cluster with non-matching name should remain unchanged")

	_, hasNewCluster := config.Clusters["new-ctx"]
	assert.False(t, hasNewCluster, "cluster should not be renamed when name doesn't match context")
}

// TestRenameKubeconfigContext_AuthInfoNameDoesNotMatchOldContext verifies that
// user entries are not renamed when their name doesn't match the old context name.
func TestRenameKubeconfigContext_AuthInfoNameDoesNotMatchOldContext(t *testing.T) {
	t.Parallel()

	input := []byte(`apiVersion: v1
kind: Config
current-context: old-ctx
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: old-ctx
contexts:
- context:
    cluster: old-ctx
    user: different-user
  name: old-ctx
users:
- name: different-user
  user:
    token: tok
`)

	result, err := k8s.RenameKubeconfigContext(input, "new-ctx")

	require.NoError(t, err)

	config, parseErr := clientcmd.Load(result)
	require.NoError(t, parseErr)
	assert.Equal(t, "new-ctx", config.CurrentContext)

	// User should NOT be renamed since its name didn't match old context
	_, hasDifferentUser := config.AuthInfos["different-user"]
	assert.True(t, hasDifferentUser, "user with non-matching name should remain unchanged")

	_, hasNewUser := config.AuthInfos["new-ctx"]
	assert.False(t, hasNewUser, "user should not be renamed when name doesn't match context")
}

// TestRenameKubeconfigContext_ClusterRefCollision verifies that the cluster
// entry is not renamed when a cluster with the desired name already exists.
func TestRenameKubeconfigContext_ClusterRefCollision(t *testing.T) {
	t.Parallel()

	input := []byte(`apiVersion: v1
kind: Config
current-context: old-ctx
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: old-ctx
- cluster:
    server: https://10.0.0.2:6443
  name: new-ctx
contexts:
- context:
    cluster: old-ctx
    user: old-ctx
  name: old-ctx
users:
- name: old-ctx
  user:
    token: tok
`)

	result, err := k8s.RenameKubeconfigContext(input, "new-ctx")
	require.NoError(t, err)

	config, parseErr := clientcmd.Load(result)
	require.NoError(t, parseErr)
	assert.Equal(t, "new-ctx", config.CurrentContext)

	_, hasOldContext := config.Contexts["old-ctx"]
	assert.False(t, hasOldContext, "old context entry should be renamed")

	ctxEntry, hasNewContext := config.Contexts["new-ctx"]
	require.True(t, hasNewContext, "renamed context entry should exist")
	assert.Equal(t, "old-ctx", ctxEntry.Cluster, "cluster rename should be skipped on collision")
	assert.Equal(t, "new-ctx", ctxEntry.AuthInfo, "user rename should still succeed")

	_, hasOldCluster := config.Clusters["old-ctx"]
	assert.True(t, hasOldCluster, "old cluster entry should remain due to collision")

	_, hasNewUser := config.AuthInfos["new-ctx"]
	assert.True(t, hasNewUser, "user entry should be renamed when there is no collision")
}

// TestRenameKubeconfigContext_AuthInfoRefCollision verifies that the user
// entry is not renamed when a user with the desired name already exists.
func TestRenameKubeconfigContext_AuthInfoRefCollision(t *testing.T) {
	t.Parallel()

	input := []byte(`apiVersion: v1
kind: Config
current-context: old-ctx
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: old-ctx
contexts:
- context:
    cluster: old-ctx
    user: old-ctx
  name: old-ctx
users:
- name: old-ctx
  user:
    token: old-token
- name: new-ctx
  user:
    token: existing-token
`)

	result, err := k8s.RenameKubeconfigContext(input, "new-ctx")

	require.NoError(t, err)

	config, parseErr := clientcmd.Load(result)
	require.NoError(t, parseErr)
	assert.Equal(t, "new-ctx", config.CurrentContext)

	// The old-ctx user should remain because "new-ctx" user already exists (collision)
	_, hasOldUser := config.AuthInfos["old-ctx"]
	assert.True(t, hasOldUser, "old user entry should remain due to collision")

	// The pre-existing "new-ctx" user should remain untouched
	_, hasNewUser := config.AuthInfos["new-ctx"]
	assert.True(t, hasNewUser, "pre-existing new-ctx user should remain")
}

// TestRenameKubeconfigContext_ContextNotFoundInMap verifies that a missing
// context entry in the map returns an appropriate error.
func TestRenameKubeconfigContext_ContextNotFoundInMap(t *testing.T) {
	t.Parallel()

	// Create a kubeconfig where current-context points to a non-existent context entry
	input := []byte(`apiVersion: v1
kind: Config
current-context: nonexistent
clusters: []
contexts: []
users: []
`)

	_, err := k8s.RenameKubeconfigContext(input, "new-ctx")

	require.Error(t, err)
	assert.ErrorIs(t, err, k8s.ErrKubeconfigContextNotFound)
}
