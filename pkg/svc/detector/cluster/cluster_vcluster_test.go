package cluster_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DetectDistributionFromContext — VCluster pattern
// ---------------------------------------------------------------------------

// TestDetectDistributionFromContext_VCluster verifies detection of vCluster
// distribution from a "vcluster-docker_<name>" context pattern.
func TestDetectDistributionFromContext_VCluster(t *testing.T) {
	t.Parallel()

	dist, clusterName, err := cluster.DetectDistributionFromContext("vcluster-docker_my-vcluster")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionVCluster, dist)
	assert.Equal(t, "my-vcluster", clusterName)
}

// TestDetectDistributionFromContext_VClusterEmpty verifies that an empty
// cluster name after the "vcluster-docker_" prefix returns ErrEmptyClusterName.
func TestDetectDistributionFromContext_VClusterEmpty(t *testing.T) {
	t.Parallel()

	_, _, err := cluster.DetectDistributionFromContext("vcluster-docker_")

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrEmptyClusterName)
}

// TestDetectDistributionFromContext_K3dEmpty verifies the empty cluster name
// path specifically for the K3d prefix "k3d-".
func TestDetectDistributionFromContext_K3dEmpty(t *testing.T) {
	t.Parallel()

	_, _, err := cluster.DetectDistributionFromContext("k3d-")

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrEmptyClusterName)
}

// TestDetectDistributionFromContext_TalosEmpty verifies the empty cluster name
// path specifically for the Talos prefix "admin@".
func TestDetectDistributionFromContext_TalosEmpty(t *testing.T) {
	t.Parallel()

	_, _, err := cluster.DetectDistributionFromContext("admin@")

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrEmptyClusterName)
}

// ---------------------------------------------------------------------------
// loadKubeContext — NoCurrentContext
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// DetectInfo — VCluster full flow
// ---------------------------------------------------------------------------

// TestDetectInfo_VClusterCluster tests the full DetectInfo flow for a VCluster
// context pattern.
func TestDetectInfo_VClusterCluster(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: vcluster-docker_my-vc
clusters:
- cluster:
    server: https://127.0.0.1:8443
  name: vcluster-docker_my-vc
contexts:
- context:
    cluster: vcluster-docker_my-vc
    user: admin
  name: vcluster-docker_my-vc
users:
- name: admin
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionVCluster, info.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, info.Provider)
	assert.Equal(t, "my-vc", info.ClusterName)
	assert.Equal(t, "vcluster-docker_my-vc", info.Context)
}

// ---------------------------------------------------------------------------
// DetectInfo — Talos cluster (admin@name pattern)
// ---------------------------------------------------------------------------

// TestDetectInfo_TalosCluster tests the full DetectInfo flow for a Talos cluster
// with a localhost server URL.
func TestDetectInfo_TalosCluster(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: admin@my-talos
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: my-talos
contexts:
- context:
    cluster: my-talos
    user: admin
  name: admin@my-talos
users:
- name: admin
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionTalos, info.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, info.Provider)
	assert.Equal(t, "my-talos", info.ClusterName)
}

// ---------------------------------------------------------------------------
// DetectInfo — Kind cluster
// ---------------------------------------------------------------------------

// TestDetectInfo_KindCluster tests the full DetectInfo flow for a Kind cluster.
func TestDetectInfo_KindCluster(t *testing.T) {
	t.Parallel()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: kind-my-kind
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-my-kind
contexts:
- context:
    cluster: kind-my-kind
    user: kind-my-kind
  name: kind-my-kind
users:
- name: kind-my-kind
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionVanilla, info.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, info.Provider)
	assert.Equal(t, "my-kind", info.ClusterName)
}

// ---------------------------------------------------------------------------
// ResolveKubeconfigPath — KUBECONFIG env with multiple paths
// ---------------------------------------------------------------------------

// TestResolveKubeconfigPath_EnvMultiplePaths verifies that when KUBECONFIG
// contains multiple colon-separated paths, only the first one is returned.
func TestResolveKubeconfigPath_EnvMultiplePaths(t *testing.T) {
	t.Setenv("KUBECONFIG", "/first/kubeconfig:/second/kubeconfig")

	resolved, err := cluster.ResolveKubeconfigPath("")

	require.NoError(t, err)
	assert.Equal(t, "/first/kubeconfig", resolved)
}

// ---------------------------------------------------------------------------
// DetectFromServerURL — Omni with empty cluster ref
// ---------------------------------------------------------------------------

// TestDetectFromServerURL_OmniEmptyClusterRef verifies that Omni detection
// with an empty kubeconfig cluster reference returns ErrEmptyClusterName.
func TestDetectFromServerURL_OmniEmptyClusterRef(t *testing.T) {
	t.Parallel()

	_, _, err := cluster.DetectFromServerURL(
		"https://myaccount.kubernetes.eu-west-1.omni.siderolabs.io",
		"",
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrEmptyClusterName)
}

// TestDetectFromServerURL_OmniSuccess verifies that a valid Omni endpoint with
// a non-empty cluster ref returns Talos distribution.
func TestDetectFromServerURL_OmniSuccess(t *testing.T) {
	t.Parallel()

	dist, name, err := cluster.DetectFromServerURL(
		"https://devantler.kubernetes.na-west-1.omni.siderolabs.io",
		"production",
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionTalos, dist)
	assert.Equal(t, "production", name)
}

// ---------------------------------------------------------------------------
// DetectCloudProvider — no credentials
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// DetectProviderFromEndpoint — additional paths
// ---------------------------------------------------------------------------

// TestDetectProviderFromEndpoint_TalosOmni verifies that a Talos distribution
// with an Omni server URL is detected as ProviderOmni.
func TestDetectProviderFromEndpoint_TalosOmni(t *testing.T) {
	t.Parallel()

	provider, err := cluster.DetectProviderFromEndpoint(
		v1alpha1.DistributionTalos,
		"https://acct.kubernetes.us-east-1.omni.siderolabs.io",
		"prod",
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.ProviderOmni, provider)
}

// TestDetectProviderFromEndpoint_TalosLocalhost verifies that a Talos cluster
// with a localhost server URL is detected as ProviderDocker.
func TestDetectProviderFromEndpoint_TalosLocalhost(t *testing.T) {
	t.Parallel()

	provider, err := cluster.DetectProviderFromEndpoint(
		v1alpha1.DistributionTalos,
		"https://127.0.0.1:6443",
		"local",
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.ProviderDocker, provider)
}

// TestDetectProviderFromEndpoint_VCluster verifies that VCluster always returns Docker.
func TestDetectProviderFromEndpoint_VCluster(t *testing.T) {
	t.Parallel()

	provider, err := cluster.DetectProviderFromEndpoint(
		v1alpha1.DistributionVCluster,
		"https://192.168.1.100:6443",
		"my-cluster",
	)

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.ProviderDocker, provider)
}

// ---------------------------------------------------------------------------
// extractHostFromURL edge cases
// ---------------------------------------------------------------------------

// TestExtractHostFromURL_EmptyHost verifies the ErrNoHostInURL error path.
func TestExtractHostFromURL_EmptyHost(t *testing.T) {
	t.Parallel()

	_, err := cluster.ExtractHostFromURL("http://")

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrNoHostInURL)
}

// TestExtractHostFromURL_ValidURLs tests various valid URLs.
//
//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestExtractHostFromURL_ValidURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		url      string
		wantHost string
	}{
		{name: "IPv4 with port", url: "https://192.168.1.1:6443", wantHost: "192.168.1.1"},
		{name: "hostname", url: "https://api.example.com:443", wantHost: "api.example.com"},
		{name: "localhost", url: "https://localhost:6443", wantHost: "localhost"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			host, err := cluster.ExtractHostFromURL(tc.url)

			require.NoError(t, err)
			assert.Equal(t, tc.wantHost, host)
		})
	}
}

// ---------------------------------------------------------------------------
// isLocalhost edge cases
// ---------------------------------------------------------------------------

// TestIsLocalhost_LoopbackIPv6 verifies that ::1 is recognized as localhost.
func TestIsLocalhost_LoopbackIPv6(t *testing.T) {
	t.Parallel()

	assert.True(t, cluster.IsLocalhost("::1"))
}

// TestIsLocalhost_NonLoopback verifies that a non-loopback IP returns false.
func TestIsLocalhost_NonLoopback(t *testing.T) {
	t.Parallel()

	assert.False(t, cluster.IsLocalhost("192.168.1.1"))
}

// TestIsLocalhost_NonIPHostname verifies that an arbitrary hostname returns false.
func TestIsLocalhost_NonIPHostname(t *testing.T) {
	t.Parallel()

	assert.False(t, cluster.IsLocalhost("api.example.com"))
}

// ---------------------------------------------------------------------------
// isOmniEndpoint edge cases
// ---------------------------------------------------------------------------

// TestIsOmniEndpoint_CaseInsensitive verifies case-insensitive matching.
func TestIsOmniEndpoint_CaseInsensitive(t *testing.T) {
	t.Parallel()

	assert.True(t, cluster.IsOmniEndpoint("acct.kubernetes.us-east-1.OMNI.SIDEROLABS.IO"))
}

// TestIsOmniEndpoint_NonOmni verifies non-Omni hostnames return false.
func TestIsOmniEndpoint_NonOmni(t *testing.T) {
	t.Parallel()

	assert.False(t, cluster.IsOmniEndpoint("api.kubernetes.io"))
}
