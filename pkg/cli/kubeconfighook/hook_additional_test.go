package kubeconfighook_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfighook"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

//nolint:gochecknoglobals // Serializes t.Chdir-based config discovery tests in this package.
var hookConfigDiscoveryMu sync.Mutex

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

	hookConfigDiscoveryMu.Lock()
	t.Cleanup(hookConfigDiscoveryMu.Unlock)

	t.Chdir(tmpDir)

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)

	// Should not panic or produce errors
	kubeconfighook.MaybeRefreshOmniKubeconfig(cmd)
}

// TestMaybeRefreshOmniKubeconfig_InitialFetch_ExplicitKubeconfigSkipped verifies
// that even on a fresh runner, an explicit --kubeconfig flag still short-circuits
// the hook (user is managing their own kubeconfig).
//
//nolint:paralleltest // Cannot use t.Parallel() because t.Chdir() is used.
func TestMaybeRefreshOmniKubeconfig_InitialFetch_ExplicitKubeconfigSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	hookConfigDiscoveryMu.Lock()
	t.Cleanup(hookConfigDiscoveryMu.Unlock)

	t.Chdir(tmpDir)

	kubeconfigPath := filepath.Join(tmpDir, "missing-kubeconfig")
	ksailYAML := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  cluster:\n" +
		"    distribution: Talos\n" +
		"    provider: Omni\n" +
		"    connection:\n" +
		"      kubeconfig: " + kubeconfigPath + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "ksail.yaml"), []byte(ksailYAML), 0o600))

	cmd := &cobra.Command{}
	cmd.Flags().String("kubeconfig", "", "")
	require.NoError(t, cmd.Flags().Set("kubeconfig", kubeconfigPath))

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	kubeconfighook.MaybeRefreshOmniKubeconfig(cmd)

	// Explicit --kubeconfig short-circuits before any Omni provider logic,
	// so neither a fetch attempt nor any warning should be emitted on either
	// stdout or stderr.
	assert.Empty(t, out.String(), "explicit --kubeconfig must skip the hook entirely")
}

// TestMaybeRefreshOmniKubeconfig_InitialFetch_MissingCredentials verifies that
// when the kubeconfig is missing and the Omni cluster name is resolvable, the
// hook attempts a fetch and surfaces a warning when credentials are not set —
// again proving the initial-fetch branch is reached instead of silent no-op.
func TestMaybeRefreshOmniKubeconfig_InitialFetch_MissingCredentials(t *testing.T) {
	tmpDir := t.TempDir()

	hookConfigDiscoveryMu.Lock()
	t.Cleanup(hookConfigDiscoveryMu.Unlock)

	t.Chdir(tmpDir)

	// Ensure no Omni credentials are present so the refresh attempt fails loudly.
	t.Setenv("OMNI_ENDPOINT", "")
	t.Setenv("OMNI_SERVICE_ACCOUNT_KEY", "")

	// Provide a talos config file that yields a cluster name so
	// resolveClusterName returns non-empty.
	talosYAML := "version: v1alpha1\n" +
		"machine:\n  type: controlplane\n" +
		"cluster:\n  clusterName: my-cluster\n  controlPlane:\n    endpoint: https://example.test:6443\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "talos.yaml"), []byte(talosYAML), 0o600))

	kubeconfigPath := filepath.Join(tmpDir, "missing-kubeconfig")
	ksailYAML := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  cluster:\n" +
		"    distribution: Talos\n" +
		"    provider: Omni\n" +
		"    distributionConfig: talos.yaml\n" +
		"    connection:\n" +
		"      kubeconfig: " + kubeconfigPath + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "ksail.yaml"), []byte(ksailYAML), 0o600))

	cmd := &cobra.Command{}

	// The hook writes via cmd.OutOrStderr(). Route both Out and Err to the
	// same buffer so the test captures whatever cobra chooses, without
	// depending on the internal precedence rule.
	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	kubeconfighook.MaybeRefreshOmniKubeconfig(cmd)

	assert.Contains(t, out.String(), "failed to refresh Omni kubeconfig",
		"initial-fetch branch must be entered and surface credential errors")

	_, statErr := os.Stat(kubeconfigPath)
	assert.True(t, os.IsNotExist(statErr), "kubeconfig must not be created when fetch fails")
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
