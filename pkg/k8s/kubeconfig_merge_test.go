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

// newKubeconfigCluster is a single-cluster kubeconfig used in merge tests.
const newKubeconfigCluster = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://new-cluster:6443
  name: new-cluster
contexts:
- context:
    cluster: new-cluster
    user: new-user
  name: new-context
current-context: new-context
users:
- name: new-user
  user:
    token: new-token
`

// existingKubeconfigCluster is an existing kubeconfig with one cluster.
const existingKubeconfigCluster = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://existing-cluster:6443
  name: existing-cluster
contexts:
- context:
    cluster: existing-cluster
    user: existing-user
  name: existing-context
current-context: existing-context
users:
- name: existing-user
  user:
    token: existing-token
`

func TestMergeKubeconfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		existingContent   string
		newContent        string
		useNestedDir      bool
		wantClusters      []string
		wantContexts      []string
		wantUsers         []string
		wantCurrentCtx    string
		wantClusterServer map[string]string
	}{
		{
			name:            "no existing file creates new",
			existingContent: "",
			newContent:      newKubeconfigCluster,
			wantClusters:    []string{"new-cluster"},
			wantContexts:    []string{"new-context"},
			wantUsers:       []string{"new-user"},
			wantCurrentCtx:  "new-context",
		},
		{
			name:            "creates parent directory if missing",
			existingContent: "",
			newContent:      newKubeconfigCluster,
			useNestedDir:    true,
			wantClusters:    []string{"new-cluster"},
			wantContexts:    []string{"new-context"},
			wantUsers:       []string{"new-user"},
			wantCurrentCtx:  "new-context",
		},
		{
			name:            "merges into existing file preserving other clusters",
			existingContent: existingKubeconfigCluster,
			newContent:      newKubeconfigCluster,
			wantClusters:    []string{"existing-cluster", "new-cluster"},
			wantContexts:    []string{"existing-context", "new-context"},
			wantUsers:       []string{"existing-user", "new-user"},
			wantCurrentCtx:  "new-context",
		},
		{
			name:            "overwrites same-named entries",
			existingContent: existingKubeconfigCluster,
			newContent: `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://updated-server:6443
  name: existing-cluster
contexts:
- context:
    cluster: existing-cluster
    user: existing-user
  name: existing-context
current-context: existing-context
users:
- name: existing-user
  user:
    token: updated-token
`,
			wantClusters:   []string{"existing-cluster"},
			wantContexts:   []string{"existing-context"},
			wantUsers:      []string{"existing-user"},
			wantCurrentCtx: "existing-context",
			wantClusterServer: map[string]string{
				"existing-cluster": "https://updated-server:6443",
			},
		},
		{
			name:            "preserves current context when new config has none",
			existingContent: existingKubeconfigCluster,
			newContent: `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://new-cluster:6443
  name: new-cluster
contexts:
- context:
    cluster: new-cluster
    user: new-user
  name: new-context
users:
- name: new-user
  user:
    token: new-token
`,
			wantClusters:   []string{"existing-cluster", "new-cluster"},
			wantContexts:   []string{"existing-context", "new-context"},
			wantUsers:      []string{"existing-user", "new-user"},
			wantCurrentCtx: "existing-context",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()

			var kubeconfigPath string
			if tc.useNestedDir {
				kubeconfigPath = filepath.Join(tmpDir, "nested", "deep", "kubeconfig")
			} else {
				kubeconfigPath = filepath.Join(tmpDir, "kubeconfig")
			}

			if tc.existingContent != "" {
				require.NoError(t, os.WriteFile(kubeconfigPath, []byte(tc.existingContent), 0o600))
			}

			err := k8s.MergeKubeconfig(kubeconfigPath, []byte(tc.newContent))
			require.NoError(t, err)

			// Read back and verify
			result, err := os.ReadFile(kubeconfigPath) //nolint:gosec // test-controlled temp path
			require.NoError(t, err)

			config, err := clientcmd.Load(result)
			require.NoError(t, err)

			// Check clusters
			clusterNames := make([]string, 0, len(config.Clusters))
			for name := range config.Clusters {
				clusterNames = append(clusterNames, name)
			}
			assert.ElementsMatch(t, tc.wantClusters, clusterNames, "clusters")

			// Check contexts
			contextNames := make([]string, 0, len(config.Contexts))
			for name := range config.Contexts {
				contextNames = append(contextNames, name)
			}
			assert.ElementsMatch(t, tc.wantContexts, contextNames, "contexts")

			// Check users
			userNames := make([]string, 0, len(config.AuthInfos))
			for name := range config.AuthInfos {
				userNames = append(userNames, name)
			}
			assert.ElementsMatch(t, tc.wantUsers, userNames, "users")

			// Check current context
			assert.Equal(t, tc.wantCurrentCtx, config.CurrentContext, "current-context")

			// Check specific server values when requested
			for name, wantServer := range tc.wantClusterServer {
				cluster, ok := config.Clusters[name]
				require.True(t, ok, "cluster %q should exist", name)
				assert.Equal(t, wantServer, cluster.Server, "cluster %q server", name)
			}
		})
	}
}

func TestMergeKubeconfig_InvalidNewData(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	err := k8s.MergeKubeconfig(kubeconfigPath, []byte("not valid yaml {{{"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse new kubeconfig")
}

func TestMergeKubeconfig_InvalidExistingFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(kubeconfigPath, []byte("not valid yaml {{{"), 0o600))

	err := k8s.MergeKubeconfig(kubeconfigPath, []byte(newKubeconfigCluster))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load existing kubeconfig")
}
