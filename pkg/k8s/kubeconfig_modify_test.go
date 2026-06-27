package k8s_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

// twoClusterKubeconfig is a kubeconfig with two cluster entries, used to verify
// that ModifyKubeconfigCluster updates only the targeted cluster and leaves the
// other entry untouched.
const twoClusterKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://cluster-a:6443
  name: cluster-a
- cluster:
    server: https://cluster-b:6443
  name: cluster-b
contexts:
- context:
    cluster: cluster-a
    user: user-a
  name: context-a
current-context: context-a
users:
- name: user-a
  user:
    token: token-a
`

// writeKubeconfigFile writes content to a fresh temp kubeconfig and returns its path.
func writeKubeconfigFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// TestModifyKubeconfigCluster_UpdatesTargetPreservesOthers verifies the happy
// path: the targeted cluster's server URL is rewritten while every other entry
// (including the second cluster) is preserved.
func TestModifyKubeconfigCluster_UpdatesTargetPreservesOthers(t *testing.T) {
	t.Parallel()

	const newServer = "https://updated-a:7443"

	path := writeKubeconfigFile(t, twoClusterKubeconfig)

	err := k8s.ModifyKubeconfigCluster(path, "cluster-a", newServer)
	require.NoError(t, err)

	cfg, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)

	require.Contains(t, cfg.Clusters, "cluster-a")
	assert.Equal(t, newServer, cfg.Clusters["cluster-a"].Server)
	// The untargeted cluster must be left intact.
	require.Contains(t, cfg.Clusters, "cluster-b")
	assert.Equal(t, "https://cluster-b:6443", cfg.Clusters["cluster-b"].Server)
	// Context and current-context are preserved.
	assert.Equal(t, "context-a", cfg.CurrentContext)
}

// TestModifyKubeconfigCluster_ClusterNotFound verifies the ErrClusterEntryNotFound
// sentinel is returned (and wrapped) when the cluster key is absent.
func TestModifyKubeconfigCluster_ClusterNotFound(t *testing.T) {
	t.Parallel()

	path := writeKubeconfigFile(t, twoClusterKubeconfig)

	err := k8s.ModifyKubeconfigCluster(path, "cluster-missing", "https://x:6443")

	require.Error(t, err)
	require.ErrorIs(t, err, k8s.ErrClusterEntryNotFound)
	assert.Contains(t, err.Error(), "cluster-missing")
}

// TestModifyKubeconfigCluster_LoadError verifies a parse failure on an existing
// but malformed kubeconfig is surfaced as a load error.
func TestModifyKubeconfigCluster_LoadError(t *testing.T) {
	t.Parallel()

	path := writeKubeconfigFile(t, "clusters: [unterminated\n")

	err := k8s.ModifyKubeconfigCluster(path, "cluster-a", "https://x:6443")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "load kubeconfig")
}

// TestModifyKubeconfigCluster_CanonicalizeError verifies a path whose parent
// component is a regular file (not a directory) fails canonicalization.
func TestModifyKubeconfigCluster_CanonicalizeError(t *testing.T) {
	t.Parallel()

	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o600))
	// The parent ("file") is a regular file, so resolving "file/config" fails
	// with ENOTDIR (not IsNotExist) inside EvalCanonicalPath.
	badPath := filepath.Join(notADir, "config")

	err := k8s.ModifyKubeconfigCluster(badPath, "cluster-a", "https://x:6443")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "canonicalize kubeconfig path")
}
