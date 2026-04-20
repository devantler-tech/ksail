package hetzner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeDeepTestKubeconfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	content := `apiVersion: v1
kind: Config
clusters:
- name: test-cluster
  cluster:
    server: https://127.0.0.1:6443
    insecure-skip-tls-verify: true
contexts:
- name: test
  context:
    cluster: test-cluster
    user: test-user
current-context: test
users:
- name: test-user
  user:
    token: fake-token
`
	path := filepath.Join(dir, "kubeconfig")
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err)

	return path
}

// TestEnsureSecret_ValidKubeconfig_NoCluster exercises the k8s.NewClientset
// success path by providing a valid kubeconfig that points to a non-reachable
// server. The function should create a client but fail when interacting with
// the cluster.
func TestEnsureSecret_ValidKubeconfig_NoCluster(t *testing.T) {
	t.Setenv(hetzner.TokenEnvVar, "test-token-for-deep-test")

	kubeconfig := writeDeepTestKubeconfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hetzner.EnsureSecret(ctx, kubeconfig, "test")
	// Should create k8s client OK but fail when trying to interact with cluster.
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "failed to create kubernetes client")
}
