package cluster_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	rootcmd "github.com/devantler-tech/ksail/v7/pkg/cli/cmd"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestForgetCommandLocalRecovery(t *testing.T) {
	t.Parallel()

	for _, shared := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "separate credentials", true: "shared credentials"}[shared],
			func(t *testing.T) {
				t.Parallel()

				var requests atomic.Int64

				server := httptest.NewServer(
					http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
						requests.Add(1)
					}),
				)
				t.Cleanup(server.Close)

				dir := t.TempDir()
				marker := filepath.Join(dir, "exec-plugin-ran")
				config := forgetFixture(server.URL, marker, shared)
				path := filepath.Join(dir, "config")
				require.NoError(t, clientcmd.WriteToFile(*config, path))
				before, err := clientcmd.LoadFromFile(path)
				require.NoError(t, err)

				output, err := executeForget(
					t,
					"--experimental",
					"--kubeconfig",
					path,
					"--context",
					"kind-nested",
				)
				require.NoError(t, err)
				assert.Contains(t, output, "Forgot local context")
				assert.Zero(t, requests.Load(), "local recovery must never contact either API")
				assert.NoFileExists(t, marker, "local recovery must never execute credentials")

				actual, err := clientcmd.LoadFromFile(path)
				require.NoError(t, err)
				delete(before.Contexts, "kind-nested")

				if !shared {
					delete(before.Clusters, "child")
					delete(before.AuthInfos, "child")
				}

				before.CurrentContext = ""
				assert.Equal(t, before, actual)

				contents, err := os.ReadFile(path)
				require.NoError(t, err)
				output, err = executeForget(
					t,
					"--experimental",
					"--kubeconfig",
					path,
					"--context",
					"kind-nested",
				)
				require.NoError(t, err)
				assert.Contains(t, output, "already absent")

				afterRetry, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(
					t,
					contents,
					afterRetry,
					"an idempotent retry must not rewrite the file",
				)
			},
		)
	}
}

func TestForgetCommandDisabledPreservesKubeconfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(
		t,
		clientcmd.WriteToFile(*forgetFixture("https://unused.invalid", "unused", false), path),
	)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = executeForget(t, "--kubeconfig", path, "--context", "kind-nested")
	require.ErrorIs(t, err, experimental.ErrDisabled)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)

	cmd := cluster.NewClusterCmd()
	forget, _, err := cmd.Find([]string{"forget"})
	require.NoError(t, err)
	assert.True(t, forget.Hidden, "experimental recovery stays out of help and generated tools")
}

func TestForgetCommandRequiresExplicitFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(
		t,
		clientcmd.WriteToFile(*forgetFixture("https://unused.invalid", "unused", false), path),
	)
	t.Setenv("KUBECONFIG", path)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	for _, args := range [][]string{
		{"--experimental"},
		{"--experimental", "--kubeconfig", path},
		{"--experimental", "--context", "kind-nested"},
		{"--experimental", "--kubeconfig", path, "--context", ""},
		{"--experimental", "--kubeconfig", "", "--context", "kind-nested"},
		{"--experimental", "--kubeconfig", path, "--context", "kind-nested", "unexpected"},
	} {
		output, err := executeForget(t, args...)
		require.Error(t, err)
		assert.NotContains(t, output, "Forgot local context")
	}

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func executeForget(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := rootcmd.NewRootCmd("test", "test", "test")
	cmd.SetArgs(append([]string{"cluster", "forget"}, args...))
	cmd.SetContext(t.Context())

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()

	return output.String(), err
}

func TestForgetCommandBypassesRootConnectionRefresh(t *testing.T) {
	t.Setenv("OMNI_ENDPOINT", "")
	t.Setenv("OMNI_SERVICE_ACCOUNT_KEY", "")
	dir := t.TempDir()
	talosPath := filepath.Join(dir, "talos.yaml")
	require.NoError(t, os.WriteFile(talosPath, []byte("version: v1alpha1\n"+
		"machine:\n  type: controlplane\ncluster:\n  clusterName: local-only\n"+
		"  controlPlane:\n    endpoint: https://unused.invalid:6443\n"), 0o600))

	missing := filepath.Join(dir, "missing-config")
	configPath := filepath.Join(dir, "ksail.yaml")
	require.NoError(
		t,
		os.WriteFile(configPath, []byte("apiVersion: ksail.io/v1alpha1\nkind: Cluster\n"+
			"spec:\n  cluster:\n    distribution: Talos\n    provider: Omni\n"+
			"    distributionConfig: "+talosPath+"\n    connection:\n      kubeconfig: "+missing+"\n"), 0o600),
	)

	for _, args := range [][]string{
		{"--config", configPath, "--context", "local-only"},
		{"--config", configPath, "--context", "local-only", "--experimental"},
		{"--config", configPath, "--kubeconfig", missing, "--context", "local-only"},
	} {
		output, err := executeForget(t, args...)
		require.Error(t, err)
		assert.NotContains(
			t,
			output,
			"Omni",
			"even invalid or disabled recovery must bypass provider hooks",
		)
		assert.NoFileExists(t, missing)
	}
}

func forgetFixture(server, marker string, shared bool) *clientcmdapi.Config {
	config := clientcmdapi.NewConfig()
	config.Clusters["host"] = &clientcmdapi.Cluster{Server: server}
	config.Clusters["child"] = &clientcmdapi.Cluster{Server: server}
	config.AuthInfos["host"] = &clientcmdapi.AuthInfo{Token: "preserved-host-token"}
	config.AuthInfos["child"] = &clientcmdapi.AuthInfo{Exec: &clientcmdapi.ExecConfig{
		APIVersion: "client.authentication.k8s.io/v1", Command: "touch", Args: []string{marker},
		InteractiveMode: clientcmdapi.NeverExecInteractiveMode,
	}}
	config.Contexts["host"] = &clientcmdapi.Context{
		Cluster:   "host",
		AuthInfo:  "host",
		Namespace: "host-space",
	}

	config.Contexts["kind-nested"] = &clientcmdapi.Context{Cluster: "child", AuthInfo: "child"}
	if shared {
		config.Contexts["another-child"] = &clientcmdapi.Context{
			Cluster:  "child",
			AuthInfo: "child",
		}
	}

	config.CurrentContext = "kind-nested"

	return config
}
