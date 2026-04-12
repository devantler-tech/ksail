package kubeconfighook_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v6/pkg/cli/kubeconfighook"
	talosconfigmanager "github.com/devantler-tech/ksail/v6/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestAtomicWriteFile exercises the atomic file write logic.
//
//nolint:gosec // Test-only fixtures use controlled temp paths and permissions.
func TestAtomicWriteFile(t *testing.T) {
	t.Parallel()

	t.Run("writes data correctly", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "test-file")
		data := []byte("hello world")

		err := kubeconfighook.AtomicWriteFileForTest(path, data, 0o600)
		require.NoError(t, err)

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "existing")

		err := os.WriteFile(path, []byte("old"), 0o600)
		require.NoError(t, err)

		err = kubeconfighook.AtomicWriteFileForTest(path, []byte("new"), 0o600)
		require.NoError(t, err)

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, []byte("new"), got)
	})

	t.Run("sets file permissions", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "perms")

		err := kubeconfighook.AtomicWriteFileForTest(path, []byte("data"), 0o600)
		require.NoError(t, err)

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("error on nonexistent directory", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nonexistent", "file")

		err := kubeconfighook.AtomicWriteFileForTest(path, []byte("data"), 0o600)
		require.Error(t, err)
	})
}

// TestResolveClusterName exercises the priority-based cluster name resolution.
func TestResolveClusterName(t *testing.T) {
	t.Parallel()

	t.Run("nil distCfg falls back to kubeconfig", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfigWithContext(t, dir, "admin@my-cluster")

		name := kubeconfighook.ResolveClusterNameForTest(nil, path)
		assert.Equal(t, "my-cluster", name)
	})

	t.Run("distCfg with Talos takes priority", func(t *testing.T) {
		t.Parallel()

		distCfg := &clusterprovisioner.DistributionConfig{
			Talos: &talosconfigmanager.Configs{Name: "from-talos"},
		}

		name := kubeconfighook.ResolveClusterNameForTest(distCfg, "")
		assert.Equal(t, "from-talos", name)
	})

	t.Run("empty distCfg falls back to kubeconfig", func(t *testing.T) {
		t.Parallel()

		distCfg := &clusterprovisioner.DistributionConfig{}

		dir := t.TempDir()
		path := writeKubeconfigWithContext(t, dir, "admin@fallback-cluster")

		name := kubeconfighook.ResolveClusterNameForTest(distCfg, path)
		assert.Equal(t, "fallback-cluster", name)
	})
}

// TestClusterNameFromDistConfig exercises the distribution config extraction.
func TestClusterNameFromDistConfig(t *testing.T) {
	t.Parallel()

	t.Run("nil returns empty", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, kubeconfighook.ClusterNameFromDistConfigForTest(nil))
	})

	t.Run("no Talos config returns empty", func(t *testing.T) {
		t.Parallel()

		distCfg := &clusterprovisioner.DistributionConfig{}
		assert.Empty(t, kubeconfighook.ClusterNameFromDistConfigForTest(distCfg))
	})

	t.Run("Talos config returns cluster name", func(t *testing.T) {
		t.Parallel()

		distCfg := &clusterprovisioner.DistributionConfig{
			Talos: &talosconfigmanager.Configs{Name: "test-cluster"},
		}
		assert.Equal(t, "test-cluster", kubeconfighook.ClusterNameFromDistConfigForTest(distCfg))
	})
}

// TestClusterNameFromKubeconfig exercises kubeconfig context extraction.
func TestClusterNameFromKubeconfig(t *testing.T) {
	t.Parallel()

	t.Run("admin@prefix extracted", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfigWithContext(t, dir, "admin@homelab")

		assert.Equal(t, "homelab", kubeconfighook.ClusterNameFromKubeconfigForTest(path))
	})

	t.Run("non-admin context returns empty", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := writeKubeconfigWithContext(t, dir, "kind-test")

		assert.Empty(t, kubeconfighook.ClusterNameFromKubeconfigForTest(path))
	})

	t.Run("missing file returns empty", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, kubeconfighook.ClusterNameFromKubeconfigForTest("/nonexistent"))
	})
}

// TestIsKubeconfigFlagExplicit exercises the flag detection logic.
func TestIsKubeconfigFlagExplicit(t *testing.T) {
	t.Parallel()

	t.Run("nil command returns false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, kubeconfighook.IsKubeconfigFlagExplicitForTest(nil))
	})

	t.Run("no kubeconfig flag returns false", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}
		assert.False(t, kubeconfighook.IsKubeconfigFlagExplicitForTest(cmd))
	})

	t.Run("unchanged flag returns false", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}
		cmd.Flags().String("kubeconfig", "", "kubeconfig path")

		assert.False(t, kubeconfighook.IsKubeconfigFlagExplicitForTest(cmd))
	})

	t.Run("changed flag returns true", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}
		cmd.Flags().String("kubeconfig", "", "kubeconfig path")

		err := cmd.Flags().Set("kubeconfig", "/some/path")
		require.NoError(t, err)

		assert.True(t, kubeconfighook.IsKubeconfigFlagExplicitForTest(cmd))
	})
}

// TestJwtExpiry exercises the JWT parsing for expiry extraction.
func TestJwtExpiry(t *testing.T) {
	t.Parallel()

	t.Run("valid JWT returns expiry", func(t *testing.T) {
		t.Parallel()

		futureExp := time.Now().Add(1 * time.Hour).Unix()
		token := makeJWT(t, futureExp)

		expiry, err := kubeconfighook.JwtExpiryForTest(token)
		require.NoError(t, err)
		assert.Equal(t, futureExp, expiry.Unix())
	})

	t.Run("not a JWT returns error", func(t *testing.T) {
		t.Parallel()

		_, err := kubeconfighook.JwtExpiryForTest("plain-token")
		require.Error(t, err)
	})

	t.Run("two segments returns error", func(t *testing.T) {
		t.Parallel()

		_, err := kubeconfighook.JwtExpiryForTest("a.b")
		require.Error(t, err)
	})

	t.Run("invalid base64 payload returns error", func(t *testing.T) {
		t.Parallel()

		_, err := kubeconfighook.JwtExpiryForTest("a.!!!.c")
		require.Error(t, err)
	})

	t.Run("invalid JSON payload returns error", func(t *testing.T) {
		t.Parallel()

		payload := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
		_, err := kubeconfighook.JwtExpiryForTest("a." + payload + ".c")
		require.Error(t, err)
	})

	t.Run("zero exp returns error", func(t *testing.T) {
		t.Parallel()

		token := makeJWT(t, 0)
		_, err := kubeconfighook.JwtExpiryForTest(token)
		require.Error(t, err)
	})
}

// TestMaybeRefreshOmniKubeconfig_NoConfig verifies that the hook is a no-op
// when no KSail config exists.
//
//nolint:paralleltest // Cannot use t.Parallel() because t.Chdir() is used.
func TestMaybeRefreshOmniKubeconfig_NoConfig(t *testing.T) {
	// Run in a temp directory with no ksail.yaml
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	cmd := &cobra.Command{}
	cmd.SetOut(os.Stdout)

	// Should not panic or produce errors
	kubeconfighook.MaybeRefreshOmniKubeconfig(cmd)
}

// writeKubeconfigWithContext creates a kubeconfig with the given current context name.
func writeKubeconfigWithContext(t *testing.T, dir, contextName string) string {
	t.Helper()

	kubeconfigPath := filepath.Join(dir, "kubeconfig")

	cfg := clientcmdapi.NewConfig()
	cfg.CurrentContext = contextName
	cfg.Clusters[contextName] = &clientcmdapi.Cluster{
		Server: "https://127.0.0.1:6443",
	}
	cfg.AuthInfos[contextName] = &clientcmdapi.AuthInfo{Token: "tok"}
	cfg.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:  contextName,
		AuthInfo: contextName,
	}

	err := clientcmd.WriteToFile(*cfg, kubeconfigPath)
	require.NoError(t, err)

	return kubeconfigPath
}
