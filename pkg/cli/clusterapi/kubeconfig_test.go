package clusterapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

const multiContextKubeconfig = `apiVersion: v1
kind: Config
current-context: kind-other
clusters:
  - name: kind-prod
    cluster:
      server: https://127.0.0.1:6443
  - name: kind-other
    cluster:
      server: https://127.0.0.1:7443
contexts:
  - name: kind-prod
    context:
      cluster: kind-prod
      user: kind-prod
  - name: kind-other
    context:
      cluster: kind-other
      user: kind-other
users:
  - name: kind-prod
    user: {}
  - name: kind-other
    user: {}
`

func TestKubeconfigExportsSingleContext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(multiContextKubeconfig), 0o600))

	service := newTestService(nil)
	service.SetKubeconfigPathForTest(path)

	out, err := service.Kubeconfig(context.Background(), "default", "prod")
	require.NoError(t, err)

	cfg, err := clientcmd.Load(out)
	require.NoError(t, err)

	// Only the prod context (and the cluster/user it references) is exported; "other" is excluded.
	assert.Equal(t, "kind-prod", cfg.CurrentContext)
	assert.Len(t, cfg.Contexts, 1)
	assert.Contains(t, cfg.Contexts, "kind-prod")
	assert.NotContains(t, cfg.Contexts, "kind-other")
	assert.Contains(t, cfg.Clusters, "kind-prod")
	assert.NotContains(t, cfg.Clusters, "kind-other")
}

func TestKubeconfigUnknownCluster(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(multiContextKubeconfig), 0o600))

	service := newTestService(nil)
	service.SetKubeconfigPathForTest(path)

	_, err := service.Kubeconfig(context.Background(), "default", "missing")
	require.ErrorIs(t, err, api.ErrNotFound)
}
