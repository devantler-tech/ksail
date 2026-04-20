package argocdinstaller_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	argocdinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/argocd"
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

// TestEnsureDefaultResources_ValidKubeconfig_NoCluster tests that when a valid
// kubeconfig is provided but no cluster is running, the function fails at the
// readiness check stage (exercises BuildRESTConfig and NewForConfig paths).
func TestEnsureDefaultResources_ValidKubeconfig_NoCluster(t *testing.T) {
	t.Parallel()

	kubeconfig := writeDeepTestKubeconfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := argocdinstaller.EnsureDefaultResources(ctx, kubeconfig, 1*time.Second)

	require.Error(t, err)
	// The error should be from waiting for resources since there's no actual cluster running.
}

// TestEnsureSopsAgeSecret_EnabledWithKey_ValidKubeconfig tests the path where
// SOPS is enabled, key is available, kubeconfig is valid, but cluster is not reachable.
func TestEnsureSopsAgeSecret_EnabledWithKey_ValidKubeconfig(t *testing.T) {
	const testKey = "AGE-SECRET-KEY-1TESTKEY000000000000000000000000000000000000000000000000"
	t.Setenv("TEST_ARGOCD_DEEP_SOPS_KEY", testKey)
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "nonexistent.txt"))

	kubeconfig := writeDeepTestKubeconfig(t)

	enabled := true
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					Enabled:      &enabled,
					AgeKeyEnvVar: "TEST_ARGOCD_DEEP_SOPS_KEY",
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := argocdinstaller.EnsureSopsAgeSecret(ctx, kubeconfig, clusterCfg)

	// Should fail when trying to connect to the non-existent cluster.
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SOPS is enabled but no Age key found")
}

// TestEnsureSopsAgeSecret_AutoDetectWithKey tests auto-detect mode where a key
// is available via env var but kubeconfig is invalid.
func TestEnsureSopsAgeSecret_AutoDetectWithKey(t *testing.T) {
	const testKey = "AGE-SECRET-KEY-1AUTOKEY00000000000000000000000000000000000000000000000"
	t.Setenv("TEST_ARGOCD_AUTO_SOPS_KEY", testKey)
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "nonexistent.txt"))

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				SOPS: v1alpha1.SOPS{
					AgeKeyEnvVar: "TEST_ARGOCD_AUTO_SOPS_KEY",
				},
			},
		},
	}

	// Auto-detect with key, but empty kubeconfig should fail at REST config stage.
	err := argocdinstaller.EnsureSopsAgeSecret(t.Context(), "", clusterCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build REST config")
}
