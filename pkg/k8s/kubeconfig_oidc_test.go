package k8s_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

// baseKubeconfig is a minimal kubeconfig with one admin cluster/context/user for OIDC tests.
const baseKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-local
contexts:
- context:
    cluster: kind-local
    user: kind-local
  name: kind-local
current-context: kind-local
users:
- name: kind-local
  user:
    token: admin-token
`

// writeKubeconfig writes content to a temp kubeconfig and returns the path.
func writeKubeconfig(t *testing.T, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	err := os.WriteFile(kubeconfigPath, []byte(content), 0o600)
	require.NoError(t, err)

	return kubeconfigPath
}

// TestAddOIDCKubeconfigEntries tests adding OIDC exec credential entries to kubeconfig.
func TestAddOIDCKubeconfigEntries(t *testing.T) { //nolint:funlen // table-driven test with multiple subtests
	t.Parallel()

	tests := []struct {
		name           string
		cfg            func(path string) *k8s.OIDCExecConfig
		wantUser       string
		wantContext    string
		wantCluster    string
		wantArgsSubset []string
		wantErr        string
	}{
		{
			name: "basic OIDC config",
			cfg: func(path string) *k8s.OIDCExecConfig {
				return &k8s.OIDCExecConfig{
					KubeconfigPath:   path,
					ClusterEntryName: "kind-local",
					DisplayName:      "local",
					IssuerURL:        "https://dex.example.com",
					ClientID:         "kubectl",
				}
			},
			wantUser:    "oidc-local",
			wantContext: "oidc@local",
			wantCluster: "kind-local",
			wantArgsSubset: []string{
				"oidc", "get-token",
				"--issuer-url=https://dex.example.com",
				"--client-id=kubectl",
			},
		},
		{
			name: "with extra scopes",
			cfg: func(path string) *k8s.OIDCExecConfig {
				return &k8s.OIDCExecConfig{
					KubeconfigPath:   path,
					ClusterEntryName: "kind-local",
					DisplayName:      "local",
					IssuerURL:        "https://dex.example.com",
					ClientID:         "kubectl",
					ExtraScopes:      []string{"email", "groups"},
				}
			},
			wantUser:    "oidc-local",
			wantContext: "oidc@local",
			wantCluster: "kind-local",
			wantArgsSubset: []string{
				"--extra-scope=email",
				"--extra-scope=groups",
			},
		},
		{
			name: "with CA file",
			cfg: func(path string) *k8s.OIDCExecConfig {
				return &k8s.OIDCExecConfig{
					KubeconfigPath:   path,
					ClusterEntryName: "kind-local",
					DisplayName:      "local",
					IssuerURL:        "https://dex.example.com",
					ClientID:         "kubectl",
					CAFile:           "/etc/ssl/certs/oidc-ca.crt",
				}
			},
			wantUser:    "oidc-local",
			wantContext: "oidc@local",
			wantCluster: "kind-local",
			wantArgsSubset: []string{
				"--ca-file=/etc/ssl/certs/oidc-ca.crt",
			},
		},
		{
			name: "nonexistent kubeconfig",
			cfg: func(_ string) *k8s.OIDCExecConfig {
				return &k8s.OIDCExecConfig{
					KubeconfigPath:   "/nonexistent/path/kubeconfig",
					ClusterEntryName: "kind-local",
					DisplayName:      "local",
					IssuerURL:        "https://dex.example.com",
					ClientID:         "kubectl",
				}
			},
			wantErr: "failed to read kubeconfig",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			kubeconfigPath := writeKubeconfig(t, baseKubeconfig)
			cfg := testCase.cfg(kubeconfigPath)

			err := k8s.AddOIDCKubeconfigEntries(cfg, io.Discard)

			if testCase.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErr)

				return
			}

			require.NoError(t, err)

			config, err := clientcmd.LoadFromFile(kubeconfigPath)
			require.NoError(t, err)

			// Verify OIDC user was added with exec config
			authInfo, hasUser := config.AuthInfos[testCase.wantUser]
			require.True(t, hasUser, "OIDC user %q should exist", testCase.wantUser)
			require.NotNil(t, authInfo.Exec, "OIDC user should have exec config")
			assert.Equal(t, "client.authentication.k8s.io/v1", authInfo.Exec.APIVersion)
			assert.Equal(t, "ksail", authInfo.Exec.Command)

			for _, wantArg := range testCase.wantArgsSubset {
				assert.Contains(t, authInfo.Exec.Args, wantArg, "exec args should contain %q", wantArg)
			}

			// Verify OIDC context was added
			ctx, hasContext := config.Contexts[testCase.wantContext]
			require.True(t, hasContext, "OIDC context %q should exist", testCase.wantContext)
			assert.Equal(t, testCase.wantCluster, ctx.Cluster, "context should reference correct cluster")
			assert.Equal(t, testCase.wantUser, ctx.AuthInfo, "context should reference OIDC user")

			// Verify admin context remains current
			assert.Equal(t, "kind-local", config.CurrentContext, "admin context should remain current")

			// Verify admin entries are preserved
			_, hasAdminUser := config.AuthInfos["kind-local"]
			assert.True(t, hasAdminUser, "admin user should be preserved")
		})
	}
}

// TestAddOIDCKubeconfigEntries_LogMessage tests that a log message is written.
func TestAddOIDCKubeconfigEntries_LogMessage(t *testing.T) {
	t.Parallel()

	kubeconfigPath := writeKubeconfig(t, baseKubeconfig)

	var logBuffer bytes.Buffer

	err := k8s.AddOIDCKubeconfigEntries(&k8s.OIDCExecConfig{
		KubeconfigPath:   kubeconfigPath,
		ClusterEntryName: "kind-local",
		DisplayName:      "local",
		IssuerURL:        "https://dex.example.com",
		ClientID:         "kubectl",
	}, &logBuffer)

	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(), "oidc@local")
}

// TestAddOIDCKubeconfigEntries_InvalidYAML tests handling of invalid kubeconfig content.
func TestAddOIDCKubeconfigEntries_InvalidYAML(t *testing.T) {
	t.Parallel()

	kubeconfigPath := writeKubeconfig(t, "this is not valid yaml {{{")

	err := k8s.AddOIDCKubeconfigEntries(&k8s.OIDCExecConfig{
		KubeconfigPath:   kubeconfigPath,
		ClusterEntryName: "kind-local",
		DisplayName:      "local",
		IssuerURL:        "https://dex.example.com",
		ClientID:         "kubectl",
	}, io.Discard)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse kubeconfig")
}

// TestCleanupOIDCKubeconfigEntries tests removing OIDC entries from kubeconfig.
func TestCleanupOIDCKubeconfigEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		displayName string
	}{
		{name: "basic cleanup", displayName: "local"},
		{name: "different cluster name", displayName: "dev-cluster"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			kubeconfigPath := writeKubeconfig(t, baseKubeconfig)

			// First add OIDC entries
			err := k8s.AddOIDCKubeconfigEntries(&k8s.OIDCExecConfig{
				KubeconfigPath:   kubeconfigPath,
				ClusterEntryName: "kind-local",
				DisplayName:      testCase.displayName,
				IssuerURL:        "https://dex.example.com",
				ClientID:         "kubectl",
			}, io.Discard)
			require.NoError(t, err)

			// Verify entries were added
			config, err := clientcmd.LoadFromFile(kubeconfigPath)
			require.NoError(t, err)

			userName := "oidc-" + testCase.displayName
			contextName := "oidc@" + testCase.displayName

			_, hasUser := config.AuthInfos[userName]
			require.True(t, hasUser, "OIDC user should exist before cleanup")

			_, hasContext := config.Contexts[contextName]
			require.True(t, hasContext, "OIDC context should exist before cleanup")

			// Cleanup OIDC entries
			err = k8s.CleanupOIDCKubeconfigEntries(kubeconfigPath, testCase.displayName, io.Discard)
			require.NoError(t, err)

			// Verify OIDC entries were removed
			config, err = clientcmd.LoadFromFile(kubeconfigPath)
			require.NoError(t, err)

			_, hasUser = config.AuthInfos[userName]
			assert.False(t, hasUser, "OIDC user should be removed after cleanup")

			_, hasContext = config.Contexts[contextName]
			assert.False(t, hasContext, "OIDC context should be removed after cleanup")

			// Verify admin entries are preserved
			_, hasAdminUser := config.AuthInfos["kind-local"]
			assert.True(t, hasAdminUser, "admin user should be preserved")

			_, hasAdminContext := config.Contexts["kind-local"]
			assert.True(t, hasAdminContext, "admin context should be preserved")
		})
	}
}

// TestCleanupOIDCKubeconfigEntries_NoOIDCEntries tests cleanup when no OIDC entries exist.
func TestCleanupOIDCKubeconfigEntries_NoOIDCEntries(t *testing.T) {
	t.Parallel()

	kubeconfigPath := writeKubeconfig(t, baseKubeconfig)

	err := k8s.CleanupOIDCKubeconfigEntries(kubeconfigPath, "local", io.Discard)

	require.NoError(t, err, "cleanup should succeed when no OIDC entries exist")

	// Verify admin entries are preserved
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	require.NoError(t, err)

	_, hasAdmin := config.AuthInfos["kind-local"]
	assert.True(t, hasAdmin, "admin user should be preserved")
}
