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
		{
			name:             "vcluster_cluster",
			contextName:      "vcluster-docker_my-vcluster",
			wantDistribution: v1alpha1.DistributionVCluster,
			wantClusterName:  "my-vcluster",
			wantError:        false,
		},
		{
			name:          "empty_vcluster_name",
			contextName:   "vcluster-docker_",
			wantError:     true,
			errorContains: "empty cluster name",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
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

// TestExtractHostFromURL tests host extraction from server URLs.
func TestExtractHostFromURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantHost  string
		wantError bool
	}{
		{
			name:     "https_ip_with_port",
			url:      "https://127.0.0.1:6443",
			wantHost: "127.0.0.1",
		},
		{
			name:     "https_hostname",
			url:      "https://localhost:6443",
			wantHost: "localhost",
		},
		{
			name:     "https_public_ip",
			url:      "https://1.2.3.4:6443",
			wantHost: "1.2.3.4",
		},
		{
			name:      "url_with_no_host",
			url:       "https://",
			wantError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			host, err := cluster.ExtractHostFromURL(testCase.url)

			if testCase.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.wantHost, host)
			}
		})
	}
}

// TestIsLocalhost tests the localhost detection logic.
func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{name: "localhost_name", host: "localhost", want: true},
		{name: "ipv4_loopback", host: "127.0.0.1", want: true},
		{name: "ipv6_loopback_short", host: "::1", want: true},
		{name: "ipv4_loopback_other", host: "127.0.0.2", want: true},
		{name: "public_ip", host: "1.2.3.4", want: false},
		{name: "private_ip", host: "192.168.1.1", want: false},
		{name: "hostname", host: "my-cluster.example.com", want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := cluster.IsLocalhost(testCase.host)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestDetectCloudProvider_NoCredentials tests cloud provider detection when no credentials are set.
func TestDetectCloudProvider_NoCredentials(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "")

	_, err := cluster.DetectCloudProvider("1.2.3.4", "my-cluster")

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrNoCloudCredentials)
}

// TestDetectProviderFromEndpoint tests provider detection for all distribution+endpoint combinations.
//
//nolint:funlen // Test function with comprehensive test cases
func TestDetectProviderFromEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		serverURL    string
		clusterName  string
		wantProvider v1alpha1.Provider
		wantError    bool
		wantErrorIs  error
	}{
		{
			name:         "vanilla_always_docker",
			distribution: v1alpha1.DistributionVanilla,
			serverURL:    "https://1.2.3.4:6443",
			wantProvider: v1alpha1.ProviderDocker,
		},
		{
			name:         "k3s_always_docker",
			distribution: v1alpha1.DistributionK3s,
			serverURL:    "https://1.2.3.4:6443",
			wantProvider: v1alpha1.ProviderDocker,
		},
		{
			name:         "vcluster_always_docker",
			distribution: v1alpha1.DistributionVCluster,
			serverURL:    "https://1.2.3.4:6443",
			wantProvider: v1alpha1.ProviderDocker,
		},
		{
			name:         "talos_localhost_is_docker",
			distribution: v1alpha1.DistributionTalos,
			serverURL:    "https://127.0.0.1:6443",
			wantProvider: v1alpha1.ProviderDocker,
		},
		{
			name:         "talos_public_ip_no_credentials",
			distribution: v1alpha1.DistributionTalos,
			serverURL:    "https://1.2.3.4:6443",
			clusterName:  "prod",
			wantError:    true,
			wantErrorIs:  cluster.ErrNoCloudCredentials,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("HCLOUD_TOKEN", "")

			provider, err := cluster.DetectProviderFromEndpoint(
				testCase.distribution,
				testCase.serverURL,
				testCase.clusterName,
			)

			if testCase.wantError {
				require.Error(t, err)
				if testCase.wantErrorIs != nil {
					require.ErrorIs(t, err, testCase.wantErrorIs)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.wantProvider, provider)
			}
		})
	}
}

// TestDetectInfo_VCluster tests detection from a kubeconfig with a VCluster cluster.
func TestDetectInfo_VCluster(t *testing.T) {
	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: vcluster-docker_my-vcluster
clusters:
- cluster:
    server: https://127.0.0.1:8443
  name: vcluster-docker_my-vcluster
contexts:
- context:
    cluster: vcluster-docker_my-vcluster
    user: vcluster-docker_my-vcluster
  name: vcluster-docker_my-vcluster
users:
- name: vcluster-docker_my-vcluster
  user:
    client-certificate-data: ""
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	info, err := cluster.DetectInfo(kubeconfigPath, "")

	require.NoError(t, err)
	assert.Equal(t, v1alpha1.DistributionVCluster, info.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, info.Provider)
	assert.Equal(t, "my-vcluster", info.ClusterName)
}

// TestDetectInfo_TalosPublicIPNoCredentials tests error when Talos cluster has public IP but no credentials.
func TestDetectInfo_TalosPublicIPNoCredentials(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "")

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: admin@prod-cluster
clusters:
- cluster:
    server: https://1.2.3.4:6443
  name: prod-cluster
contexts:
- context:
    cluster: prod-cluster
    user: admin@prod-cluster
  name: admin@prod-cluster
users:
- name: admin@prod-cluster
  user:
    client-certificate-data: ""
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)

	_, err = cluster.DetectInfo(kubeconfigPath, "")

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrNoCloudCredentials)
}

// TestResolveKubeconfigPath tests kubeconfig path resolution.
func TestResolveKubeconfigPath(t *testing.T) {
	t.Run("explicit_path_returned_as_is", func(t *testing.T) {
		tmpDir := t.TempDir()
		explicitPath := filepath.Join(tmpDir, "my-kubeconfig")
		err := os.WriteFile(explicitPath, []byte(""), 0o600)
		require.NoError(t, err)

		resolved, err := cluster.ResolveKubeconfigPath(explicitPath)

		require.NoError(t, err)
		assert.Equal(t, explicitPath, resolved)
	})

	t.Run("kubeconfig_env_var_used_when_empty_path", func(t *testing.T) {
		tmpDir := t.TempDir()
		envPath := filepath.Join(tmpDir, "env-kubeconfig")
		err := os.WriteFile(envPath, []byte(""), 0o600)
		require.NoError(t, err)

		t.Setenv("KUBECONFIG", envPath)

		resolved, err := cluster.ResolveKubeconfigPath("")

		require.NoError(t, err)
		assert.Equal(t, envPath, resolved)
	})

	t.Run("kubeconfig_env_multiple_paths_uses_first", func(t *testing.T) {
		tmpDir := t.TempDir()
		firstPath := filepath.Join(tmpDir, "first-kubeconfig")
		secondPath := filepath.Join(tmpDir, "second-kubeconfig")

		t.Setenv("KUBECONFIG", firstPath+string(os.PathListSeparator)+secondPath)

		resolved, err := cluster.ResolveKubeconfigPath("")

		require.NoError(t, err)
		assert.Equal(t, firstPath, resolved)
	})

	t.Run("defaults_to_recommended_home_file", func(t *testing.T) {
		t.Setenv("KUBECONFIG", "")

		resolved, err := cluster.ResolveKubeconfigPath("")

		require.NoError(t, err)
		assert.NotEmpty(t, resolved)
	})
}
