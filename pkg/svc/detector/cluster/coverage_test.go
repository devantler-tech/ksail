package cluster_test

import (
	"os"
	"path/filepath"
	"testing"

	cluster "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetectInfo_NonExistentKubeconfig verifies error when kubeconfig file does
// not exist.
func TestDetectInfo_NonExistentKubeconfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "does-not-exist")

	_, err := cluster.DetectInfo(nonExistentPath, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read kubeconfig")
}

// TestDetectInfo_MalformedKubeconfig verifies error when kubeconfig contains
// invalid binary data that cannot be parsed.
func TestDetectInfo_MalformedKubeconfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	// Write truly invalid content: binary data that can't be parsed as kubeconfig
	err := os.WriteFile(kubeconfigPath, []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE}, 0o600)
	require.NoError(t, err)

	_, err = cluster.DetectInfo(kubeconfigPath, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse kubeconfig")
}

// TestDetectInfo_ClusterNotFound verifies error when context references a
// cluster that doesn't exist in kubeconfig.
func TestDetectInfo_ClusterNotFound(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: kind-my-cluster
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: different-cluster
contexts:
- context:
    cluster: missing-cluster-ref
    user: kind-my-cluster
  name: kind-my-cluster
users:
- name: kind-my-cluster
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	_, err = cluster.DetectInfo(kubeconfigPath, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster not found")
}

// TestDetectInfo_K3dCluster tests detection from a kubeconfig with a K3d cluster.
func TestDetectInfo_K3dCluster(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: k3d-my-k3d
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: k3d-my-k3d
contexts:
- context:
    cluster: k3d-my-k3d
    user: admin@k3d-my-k3d
  name: k3d-my-k3d
users:
- name: admin@k3d-my-k3d
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	require.NoError(t, err)
	assert.Equal(t, "K3s", string(info.Distribution))
	assert.Equal(t, "Docker", string(info.Provider))
	assert.Equal(t, "my-k3d", info.ClusterName)
}

// TestDetectInfo_UnrecognizedContextNonOmniServerURL verifies error path
// when context doesn't match any pattern and server URL is not Omni.
func TestDetectInfo_UnrecognizedContextNonOmniServerURL(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: minikube
clusters:
- cluster:
    server: https://192.168.49.2:8443
  name: minikube
contexts:
- context:
    cluster: minikube
    user: minikube
  name: minikube
users:
- name: minikube
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	_, err = cluster.DetectInfo(kubeconfigPath, "")

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrUnknownContextPattern)
}

// TestDetectFromServerURL_InvalidURL verifies error when server URL is completely invalid.
func TestDetectFromServerURL_InvalidURL(t *testing.T) {
	t.Parallel()

	_, _, err := cluster.DetectFromServerURL("://invalid", "cluster")

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrUnknownContextPattern)
}

// TestDetectProviderFromEndpoint_TalosInvalidURL verifies error when Talos
// has an unparseable server URL.
func TestDetectProviderFromEndpoint_TalosInvalidURL(t *testing.T) {
	t.Parallel()

	_, err := cluster.DetectProviderFromEndpoint(
		"Talos",
		"://invalid",
		"cluster",
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse server URL")
}

// TestDetectCloudProvider_HetznerTokenSetButIPNotFound tests the branch where
// HCLOUD_TOKEN is set but the IP is not found in any provider.
func TestDetectCloudProvider_HetznerTokenSetButIPNotFound(t *testing.T) {
	// This test exercises the ErrUnableToDetectProvider path when
	// HCLOUD_TOKEN is set but checkHetznerOwnership fails.
	t.Setenv("HCLOUD_TOKEN", "test-token-that-will-fail")

	_, err := cluster.DetectCloudProvider("192.0.2.1", "nonexistent-cluster")

	require.Error(t, err)
	// Should get ErrUnableToDetectProvider since token is set but IP not found
	assert.ErrorIs(t, err, cluster.ErrUnableToDetectProvider)
}

// TestResolveKubeconfigPath_TildeExpansion verifies that ~ is expanded.
func TestResolveKubeconfigPath_TildeExpansion(t *testing.T) {
	t.Parallel()

	resolved, err := cluster.ResolveKubeconfigPath("~/test-kubeconfig")
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, "test-kubeconfig"), resolved)
}

// TestResolveKubeconfigPath_EnvVarTildeExpansion verifies ~ expansion in KUBECONFIG env.
func TestResolveKubeconfigPath_EnvVarTildeExpansion(t *testing.T) {
	t.Setenv("KUBECONFIG", "~/my-kubeconfig")

	resolved, err := cluster.ResolveKubeconfigPath("")
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, "my-kubeconfig"), resolved)
}

// TestDetectInfo_OmniEmptyClusterRef tests that empty cluster ref with Omni endpoint
// falls through to detect from server URL with empty cluster name.
func TestDetectInfo_OmniEmptyClusterRef(t *testing.T) {
	t.Parallel()

	// When context doesn't match a known prefix but server is Omni,
	// and the kubeconfig cluster ref is a real name, detection should succeed.
	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: service-account@omni
clusters:
- cluster:
    server: https://devantler.kubernetes.na-west-1.omni.siderolabs.io
  name: my-cluster
contexts:
- context:
    cluster: my-cluster
    user: service-account@omni
  name: service-account@omni
users:
- name: service-account@omni
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	// service-account@omni matches admin@ prefix → "omni" → Talos distribution
	// But server URL is Omni → provider should be Omni
	require.NoError(t, err)
	assert.Equal(t, "Talos", string(info.Distribution))
	assert.Equal(t, "Omni", string(info.Provider))
}

// TestResolveKubeconfigPath_EnvVarEmptyFirstPath tests KUBECONFIG with leading separator.
func TestResolveKubeconfigPath_EnvVarEmptyFirstPath(t *testing.T) {
	t.Setenv("KUBECONFIG", "")

	resolved, err := cluster.ResolveKubeconfigPath("")
	require.NoError(t, err)
	assert.NotEmpty(t, resolved)
}

// TestDetectInfo_EmptyContextWithCurrentContext tests that when contextName is
// empty, the current-context is used.
func TestDetectInfo_EmptyContextWithCurrentContext(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: k3d-my-cluster
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: k3d-my-cluster
contexts:
- context:
    cluster: k3d-my-cluster
    user: admin
  name: k3d-my-cluster
users:
- name: admin
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	require.NoError(t, err)
	assert.Equal(t, "K3s", string(info.Distribution))
	assert.Equal(t, "my-cluster", info.ClusterName)
	assert.Equal(t, kubeconfigPath, info.KubeconfigPath)
}

// TestDetectInfo_EmptyClusterNameContext tests context with known prefix but
// no actual cluster name after the prefix (e.g., "kind-").
func TestDetectInfo_EmptyClusterNameContext(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: default
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: default
contexts:
- context:
    cluster: default
    user: admin
  name: default
- context:
    cluster: default
    user: admin
  name: kind-
users:
- name: admin
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	// Use the "kind-" context explicitly
	_, err = cluster.DetectInfo(kubeconfigPath, "kind-")

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrEmptyClusterName)
}

// TestDetectInfo_OmniWithServerURLFallback tests the server URL fallback when
// context does not match admin@<name> but Omni server is detected.
func TestDetectInfo_OmniWithServerURLFallback(t *testing.T) {
	t.Parallel()

	// Context name "some-service-account" doesn't match any known prefix,
	// so it falls through to detectFromServerURL which recognizes Omni
	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: some-service-account
clusters:
- cluster:
    server: https://myaccount.kubernetes.eu-west-1.omni.siderolabs.io
  name: production-cluster
contexts:
- context:
    cluster: production-cluster
    user: some-service-account
  name: some-service-account
users:
- name: some-service-account
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	require.NoError(t, err)
	assert.Equal(t, "Talos", string(info.Distribution))
	assert.Equal(t, "Omni", string(info.Provider))
	assert.Equal(t, "production-cluster", info.ClusterName)
}
