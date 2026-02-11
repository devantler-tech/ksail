package cluster_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetectDistributionFromContext tests the distribution detection from context patterns.
//
//nolint:funlen // Test function with comprehensive test cases
func TestDetectDistributionFromContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		contextName      string
		wantDistribution v1alpha1.Distribution
		wantClusterName  string
		wantError        bool
		errorContains    string
	}{
		{
			name:             "kind_cluster",
			contextName:      "kind-my-cluster",
			wantDistribution: v1alpha1.DistributionVanilla,
			wantClusterName:  "my-cluster",
			wantError:        false,
		},
		{
			name:             "k3d_cluster",
			contextName:      "k3d-dev-cluster",
			wantDistribution: v1alpha1.DistributionK3s,
			wantClusterName:  "dev-cluster",
			wantError:        false,
		},
		{
			name:             "talos_cluster",
			contextName:      "admin@prod-cluster",
			wantDistribution: v1alpha1.DistributionTalos,
			wantClusterName:  "prod-cluster",
			wantError:        false,
		},
		{
			name:          "unknown_pattern",
			contextName:   "minikube",
			wantError:     true,
			errorContains: "unknown distribution",
		},
		{
			name:          "empty_kind_name",
			contextName:   "kind-",
			wantError:     true,
			errorContains: "empty cluster name",
		},
		{
			name:          "empty_k3d_name",
			contextName:   "k3d-",
			wantError:     true,
			errorContains: "empty cluster name",
		},
		{
			name:          "empty_talos_name",
			contextName:   "admin@",
			wantError:     true,
			errorContains: "empty cluster name",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dist, clusterName, err := cluster.DetectDistributionFromContext(testCase.contextName)

			if testCase.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errorContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.wantDistribution, dist)
				assert.Equal(t, testCase.wantClusterName, clusterName)
			}
		})
	}
}

// TestDetectInfo_LocalKind tests detection from a kubeconfig with a Kind cluster.
func TestDetectInfo_LocalKind(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: kind-test-cluster
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-test-cluster
contexts:
- context:
    cluster: kind-test-cluster
    user: kind-test-cluster
  name: kind-test-cluster
users:
- name: kind-test-cluster
  user:
    client-certificate-data: ""
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionVanilla, info.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, info.Provider)
	assert.Equal(t, "test-cluster", info.ClusterName)
	assert.Equal(t, "https://127.0.0.1:6443", info.ServerURL)
}

// TestDetectInfo_LocalTalos tests detection from a kubeconfig with a local Talos cluster.
func TestDetectInfo_LocalTalos(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: admin@local-talos
clusters:
- cluster:
    server: https://localhost:6443
  name: local-talos
contexts:
- context:
    cluster: local-talos
    user: admin@local-talos
  name: admin@local-talos
users:
- name: admin@local-talos
  user:
    client-certificate-data: ""
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionTalos, info.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, info.Provider)
	assert.Equal(t, "local-talos", info.ClusterName)
}

// TestDetectInfo_ExplicitContext tests detection with an explicit context specified.
func TestDetectInfo_ExplicitContext(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: kind-default
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-default
- cluster:
    server: https://127.0.0.1:7443
  name: kind-other
contexts:
- context:
    cluster: kind-default
    user: kind-default
  name: kind-default
- context:
    cluster: kind-other
    user: kind-other
  name: kind-other
users:
- name: kind-default
- name: kind-other
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "kind-other")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionVanilla, info.Distribution)
	assert.Equal(t, "other", info.ClusterName)
	assert.Equal(t, "https://127.0.0.1:7443", info.ServerURL)
}

// TestDetectInfo_ContextNotFound tests error when context doesn't exist.
func TestDetectInfo_ContextNotFound(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: kind-exists
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-exists
contexts:
- context:
    cluster: kind-exists
    user: kind-exists
  name: kind-exists
users:
- name: kind-exists
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	_, err = cluster.DetectInfo(kubeconfigPath, "kind-nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context not found")
}

// TestDetectInfo_NoCurrentContext tests error when no current context is set.
func TestDetectInfo_NoCurrentContext(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-test
contexts:
- context:
    cluster: kind-test
    user: kind-test
  name: kind-test
users:
- name: kind-test
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	_, err = cluster.DetectInfo(kubeconfigPath, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no current context")
}
