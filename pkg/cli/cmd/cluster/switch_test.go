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

func TestSwitchCmd_HappyPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd := &cobra.Command{Use: "switch"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := clusterpkg.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := clusterpkg.HandleSwitchRunE(cmd, "kind-staging", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Switched to cluster 'kind-staging'")

	// Verify the kubeconfig was actually updated by parsing it
	//nolint:gosec // G304: test-controlled path from t.TempDir()
	updatedBytes, err := os.ReadFile(kubeconfigPath)
	require.NoError(t, err)

	config, err := clientcmd.Load(updatedBytes)
	require.NoError(t, err)

	assert.Equal(t, "kind-staging", config.CurrentContext)
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

	cmd := &cobra.Command{Use: "switch"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := clusterpkg.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := clusterpkg.HandleSwitchRunE(cmd, "nonexistent-cluster", deps)
	require.Error(t, err)
	require.ErrorIs(t, err, clusterpkg.ErrContextNotFound)
	assert.Contains(t, err.Error(), "nonexistent-cluster")
}

func TestSwitchCmd_KubeconfigNotFound(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "switch"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

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

	cmd := &cobra.Command{Use: "switch"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := clusterpkg.SwitchDeps{KubeconfigPath: kubeconfigPath}

	// Switch to the context that's already current
	err := clusterpkg.HandleSwitchRunE(cmd, "kind-dev", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Switched to cluster 'kind-dev'")
}
