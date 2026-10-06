package workload

import (
	"bytes"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

//nolint:paralleltest // This test overrides the shared terminal and confirmation input seams.
func TestWiredCLIReadRenewsSelectedLegacySSO(t *testing.T) {
	fixture := testutil.NewSyntheticSSO(t, "legacy")

	var requests atomic.Int32

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/version", r.URL.Path)
		assert.Equal(t, "Bearer synthetic-only", r.Header.Get("Authorization"))
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"major":"1","minor":"36","gitVersion":"v1.36.0-synthetic"}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	path, before := syntheticReadKubeconfig(t, fixture, server)

	var output, stderr bytes.Buffer

	client := kubectl.NewClient(
		genericiooptions.IOStreams{In: strings.NewReader(""), Out: &output, ErrOut: &stderr},
	)
	command := client.CreateGetCommand(path)
	wrapWithKubeconfigResolution(command)

	root := &cobra.Command{Use: "ksail"}
	root.PersistentFlags().Bool(flags.ExperimentalFlagName, false, "")
	root.AddCommand(command)
	command.SetErr(&stderr)

	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return true })
	defer restoreTTY()

	restoreInput := confirm.SetStdinReaderForTests(strings.NewReader("yes\n"))
	defer restoreInput()

	defer overrideTerminal(true)()

	root.SetArgs(
		[]string{
			"get",
			"--experimental",
			"--kubeconfig",
			path,
			"--context",
			"selected",
			"--raw=/version",
		},
	)
	require.NoError(t, root.Execute(), stderr.String())
	assert.Contains(t, output.String(), "v1.36.0-synthetic")
	assert.Equal(t, int32(1), requests.Load(), "only the explicit selected context's read is sent")
	assert.NotContains(t, stderr.String(), "SENSITIVE")
	assert.Contains(t, stderr.String(), "SYNTHETIC-CODE",
		"the terminal shows the provider's instructions for signing in from another device")

	//nolint:gosec // Verify the same test-created kubeconfig was not rewritten by renewal.
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after, "renewal must not rewrite kubeconfig or switch current-context")
}

//nolint:paralleltest // Subtests override the shared terminal checker and must remain serial.
func TestCLIRecoveryDisabledOrNoninteractiveNeverProbesOrLogsIn(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "noninteractive", true: "disabled"}[interactive],
			func(t *testing.T) {
				defer overrideTerminal(true)()

				fixture := testutil.NewSyntheticSSO(t, "legacy")
				command := &cobra.Command{Use: "get"}
				command.Flags().Bool(flags.ExperimentalFlagName, !interactive, "")
				// No kubeconfig flags: reaching resolution would fail, proving the gate precedes probes.
				restore := confirm.SetTTYCheckerForTests(func() bool { return interactive })
				defer restore()

				require.NoError(t, maybeRenewAWSSSO(command))

				_, err := os.Stat(filepath.Join(fixture.Root, "logins"))
				assert.ErrorIs(t, err, os.ErrNotExist)
			},
		)
	}
}

//nolint:paralleltest // This test overrides the shared terminal seams.
func TestCLIRecoveryWithRedirectedOutputNeverProbesOrLogsIn(t *testing.T) {
	fixture := testutil.NewSyntheticSSO(t, "legacy")
	command := &cobra.Command{Use: "get"}
	command.Flags().Bool(flags.ExperimentalFlagName, true, "")
	// No kubeconfig flags: reaching resolution would fail, proving the gate precedes probes.
	restore := confirm.SetTTYCheckerForTests(func() bool { return true })
	defer restore()

	var stderr bytes.Buffer

	command.SetErr(&stderr) // A captured stream is not a terminal.

	require.NoError(t, maybeRenewAWSSSO(command))
	assert.Empty(t, stderr.String(), "no confirmation is written where the user cannot see it")

	_, err := os.Stat(filepath.Join(fixture.Root, "logins"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// overrideTerminal replaces terminal detection and returns the function that restores it.
func overrideTerminal(terminal bool) func() {
	previous := isTerminal
	isTerminal = func(io.Writer) bool { return terminal }

	return func() { isTerminal = previous }
}

func syntheticReadKubeconfig(
	t *testing.T,
	fixture testutil.SyntheticSSO,
	server *httptest.Server,
) (string, []byte) {
	t.Helper()

	certificate := pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw},
	)
	path := filepath.Join(fixture.Root, "kubeconfig")
	cfg := clientcmdapi.Config{
		CurrentContext: "unselected",
		Clusters: map[string]*clientcmdapi.Cluster{
			"selected":   {Server: server.URL, CertificateAuthorityData: certificate},
			"unselected": {Server: "https://127.0.0.1:1"},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"selected":   {Cluster: "selected", AuthInfo: "selected"},
			"unselected": {Cluster: "unselected", AuthInfo: "unused"},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"selected": {Exec: fixture.Provider}},
	}
	require.NoError(t, clientcmd.WriteToFile(cfg, path))
	//nolint:gosec // The caller supplied this test-created temporary kubeconfig path.
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	return path, before
}
