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

// overwriteKubeconfigCluster updates an existing cluster entry with a new server.
const overwriteKubeconfigCluster = `apiVersion: v1
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
`

// noCurrentContextKubeconfigCluster is a kubeconfig without current-context set.
const noCurrentContextKubeconfigCluster = `apiVersion: v1
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
`

type mergeKubeconfigTestCase struct {
	name              string
	existingContent   string
	newContent        string
	useNestedDir      bool
	wantClusters      []string
	wantContexts      []string
	wantUsers         []string
	wantCurrentCtx    string
	wantClusterServer map[string]string
}

func mergeKubeconfigTests() []mergeKubeconfigTestCase {
	return []mergeKubeconfigTestCase{
		{
			name:           "no existing file creates new",
			newContent:     newKubeconfigCluster,
			wantClusters:   []string{"new-cluster"},
			wantContexts:   []string{"new-context"},
			wantUsers:      []string{"new-user"},
			wantCurrentCtx: "new-context",
		},
		{
			name:           "creates parent directory if missing",
			newContent:     newKubeconfigCluster,
			useNestedDir:   true,
			wantClusters:   []string{"new-cluster"},
			wantContexts:   []string{"new-context"},
			wantUsers:      []string{"new-user"},
			wantCurrentCtx: "new-context",
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
			newContent:      overwriteKubeconfigCluster,
			wantClusters:    []string{"existing-cluster"},
			wantContexts:    []string{"existing-context"},
			wantUsers:       []string{"existing-user"},
			wantCurrentCtx:  "existing-context",
			wantClusterServer: map[string]string{
				"existing-cluster": "https://updated-server:6443",
			},
		},
		{
			name:            "preserves current context when new config has none",
			existingContent: existingKubeconfigCluster,
			newContent:      noCurrentContextKubeconfigCluster,
			wantClusters:    []string{"existing-cluster", "new-cluster"},
			wantContexts:    []string{"existing-context", "new-context"},
			wantUsers:       []string{"existing-user", "new-user"},
			wantCurrentCtx:  "existing-context",
		},
	}
}

func TestMergeKubeconfig(t *testing.T) {
	t.Parallel()

	for _, testCase := range mergeKubeconfigTests() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runMergeKubeconfigTest(t, testCase)
		})
	}
}

func runMergeKubeconfigTest(t *testing.T, testCase mergeKubeconfigTestCase) {
	t.Helper()

	tmpDir := t.TempDir()

	var kubeconfigPath string
	if testCase.useNestedDir {
		kubeconfigPath = filepath.Join(tmpDir, "nested", "deep", "kubeconfig")
	} else {
		kubeconfigPath = filepath.Join(tmpDir, "kubeconfig")
	}

	if testCase.existingContent != "" {
		require.NoError(t, os.WriteFile(kubeconfigPath, []byte(testCase.existingContent), 0o600))
	}

	err := k8s.MergeKubeconfig(kubeconfigPath, []byte(testCase.newContent))
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

	assert.ElementsMatch(t, testCase.wantClusters, clusterNames, "clusters")

	// Check contexts
	contextNames := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		contextNames = append(contextNames, name)
	}

	assert.ElementsMatch(t, testCase.wantContexts, contextNames, "contexts")

	// Check users
	userNames := make([]string, 0, len(config.AuthInfos))
	for name := range config.AuthInfos {
		userNames = append(userNames, name)
	}

	assert.ElementsMatch(t, testCase.wantUsers, userNames, "users")

	// Check current context
	assert.Equal(t, testCase.wantCurrentCtx, config.CurrentContext, "current-context")

	// Check specific server values when requested
	for name, wantServer := range testCase.wantClusterServer {
		cluster, ok := config.Clusters[name]
		require.True(t, ok, "cluster %q should exist", name)
		assert.Equal(t, wantServer, cluster.Server, "cluster %q server", name)
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
