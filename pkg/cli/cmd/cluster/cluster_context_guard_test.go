package cluster_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const guardKubeconfigYAML = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: prod
contexts:
- context:
    cluster: prod
    user: prod
  name: admin@prod
current-context: admin@prod
users:
- name: prod
  user:
    token: t
`

func writeGuardKubeconfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(guardKubeconfigYAML), 0o600))

	return path
}

func newClusterWithConnection(kubeconfigPath, contextName string) *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Connection.Kubeconfig = kubeconfigPath
	cfg.Spec.Cluster.Connection.Context = contextName

	return cfg
}

// TestEnsureConfiguredContextResolvable_NoContextConfigured verifies that when
// no context is pinned in spec.cluster.connection.context, the guard is a no-op
// (KSail derives the context itself, so there is nothing to validate).
func TestEnsureConfiguredContextResolvable_NoContextConfigured(t *testing.T) {
	t.Parallel()

	cfg := newClusterWithConnection("/does/not/exist", "")

	require.NoError(t, cluster.ExportEnsureConfiguredContextResolvable(cfg))
}

// TestEnsureConfiguredContextResolvable_Present verifies that a pinned context
// present in the kubeconfig passes the guard.
func TestEnsureConfiguredContextResolvable_Present(t *testing.T) {
	t.Parallel()

	cfg := newClusterWithConnection(writeGuardKubeconfig(t), "admin@prod")

	require.NoError(t, cluster.ExportEnsureConfiguredContextResolvable(cfg))
}

// TestEnsureConfiguredContextResolvable_Missing verifies that a pinned context
// absent from the kubeconfig fails fast with ErrKubeconfigContextNotFound,
// rather than letting cluster update report a misleading "No changes detected"
// after the GitOps drift probes silently fail.
func TestEnsureConfiguredContextResolvable_Missing(t *testing.T) {
	t.Parallel()

	cfg := newClusterWithConnection(writeGuardKubeconfig(t), "admin@staging")

	err := cluster.ExportEnsureConfiguredContextResolvable(cfg)

	require.ErrorIs(t, err, k8s.ErrKubeconfigContextNotFound)
	assert.Contains(t, err.Error(), "admin@staging")
}

// TestResolveKubeContext_PrefersConfiguredContext verifies that the pinned
// spec.cluster.connection.context is what drift detection resolves to, so it
// targets the configured cluster rather than the ambient current-context.
func TestResolveKubeContext_PrefersConfiguredContext(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Connection.Context = "admin@prod"

	got := cluster.ExportResolveKubeContext(&localregistry.Context{ClusterCfg: cfg})

	assert.Equal(t, "admin@prod", got)
}

// TestResolveKubeContext_TrimsConfiguredContext verifies that a whitespace-padded
// pinned context is trimmed, so the value the drift probes use to build REST
// clients matches what ensureConfiguredContextResolvable validated (which trims).
// Otherwise " admin@prod " would pass the guard yet fail the probes, reintroducing
// the very warnings this guard exists to remove.
func TestResolveKubeContext_TrimsConfiguredContext(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Connection.Context = "  admin@prod  "

	got := cluster.ExportResolveKubeContext(&localregistry.Context{ClusterCfg: cfg})

	assert.Equal(t, "admin@prod", got)
}
