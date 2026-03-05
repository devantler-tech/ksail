package cluster_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

const testKubeconfigTwoContexts = `apiVersion: v1
kind: Config
current-context: kind-dev
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-dev
- cluster:
    server: https://127.0.0.1:7443
  name: kind-staging
contexts:
- context:
    cluster: kind-dev
    user: kind-dev
  name: kind-dev
- context:
    cluster: kind-staging
    user: kind-staging
  name: kind-staging
users:
- name: kind-dev
  user: {}
- name: kind-staging
  user: {}
`

const testKubeconfigMultiDistro = `apiVersion: v1
kind: Config
current-context: kind-myapp
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-myapp
- cluster:
    server: https://127.0.0.1:7443
  name: k3d-myapp
contexts:
- context:
    cluster: kind-myapp
    user: kind-myapp
  name: kind-myapp
- context:
    cluster: k3d-myapp
    user: k3d-myapp
  name: k3d-myapp
users:
- name: kind-myapp
  user: {}
- name: k3d-myapp
  user: {}
`

func newSwitchTestCmd() (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "switch"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	return cmd, &buf
}

func TestSwitchCmd_HappyPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := clusterpkg.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := clusterpkg.HandleSwitchRunE(cmd, "staging", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"Switched to cluster 'staging' (context: kind-staging)")

	//nolint:gosec // G304: test-controlled path from t.TempDir()
	updatedBytes, err := os.ReadFile(kubeconfigPath)
	require.NoError(t, err)

	config, err := clientcmd.Load(updatedBytes)
	require.NoError(t, err)

	assert.Equal(t, "kind-staging", config.CurrentContext)
}

func TestSwitchCmd_K3sDistribution(t *testing.T) {
	t.Parallel()

	kubeconfig := `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: k3d-prod
contexts:
- context:
    cluster: k3d-prod
    user: k3d-prod
  name: k3d-prod
users:
- name: k3d-prod
  user: {}
`

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath, []byte(kubeconfig), 0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := clusterpkg.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := clusterpkg.HandleSwitchRunE(cmd, "prod", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"context: k3d-prod")
}

func TestSwitchCmd_TalosDistribution(t *testing.T) {
	t.Parallel()

	kubeconfig := `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: talos-cluster
contexts:
- context:
    cluster: talos-cluster
    user: admin@talos-cluster
  name: admin@talos-cluster
users:
- name: admin@talos-cluster
  user: {}
`

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath, []byte(kubeconfig), 0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := clusterpkg.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := clusterpkg.HandleSwitchRunE(cmd, "talos-cluster", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"context: admin@talos-cluster")
}

func TestSwitchCmd_ContextNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, _ := newSwitchTestCmd()
	deps := clusterpkg.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := clusterpkg.HandleSwitchRunE(cmd, "nonexistent", deps)
	require.Error(t, err)
	require.ErrorIs(t, err, clusterpkg.ErrContextNotFound)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "available contexts")
}

func TestSwitchCmd_AmbiguousCluster(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigMultiDistro),
		0o600,
	))

	cmd, _ := newSwitchTestCmd()
	deps := clusterpkg.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := clusterpkg.HandleSwitchRunE(cmd, "myapp", deps)
	require.Error(t, err)
	require.ErrorIs(t, err, clusterpkg.ErrAmbiguousCluster)
	assert.Contains(t, err.Error(), "k3d-myapp")
	assert.Contains(t, err.Error(), "kind-myapp")
}

func TestSwitchCmd_KubeconfigNotFound(t *testing.T) {
	t.Parallel()

	cmd, _ := newSwitchTestCmd()

	deps := clusterpkg.SwitchDeps{
		KubeconfigPath: "/nonexistent/path/kubeconfig",
	}

	err := clusterpkg.HandleSwitchRunE(cmd, "some-cluster", deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read kubeconfig")
}

func TestSwitchCmd_SameContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := clusterpkg.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := clusterpkg.HandleSwitchRunE(cmd, "dev", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"Switched to cluster 'dev'")
}
