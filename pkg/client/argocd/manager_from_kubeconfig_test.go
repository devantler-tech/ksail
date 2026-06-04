package argocd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	"github.com/stretchr/testify/require"
)

const managerKubeconfigYAML = `apiVersion: v1
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

// TestNewManagerFromKubeconfig_UsesConfiguredContext verifies that the kube
// context argument is applied when building the REST config. A pinned context
// absent from the kubeconfig must fail rather than silently falling back to the
// current-context — which is what let cluster update query the wrong cluster.
func TestNewManagerFromKubeconfig_UsesConfiguredContext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(managerKubeconfigYAML), 0o600))

	// The configured context exists -> the manager builds successfully.
	mgr, err := argocd.NewManagerFromKubeconfig(path, "admin@prod")
	require.NoError(t, err)
	require.NotNil(t, mgr)

	// A pinned context absent from the kubeconfig must error, proving the
	// override is honored instead of the ambient current-context.
	_, err = argocd.NewManagerFromKubeconfig(path, "admin@staging")
	require.Error(t, err)
}
