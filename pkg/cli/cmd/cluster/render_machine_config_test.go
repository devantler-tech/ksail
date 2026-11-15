package cluster_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// renderFixtureSource is the Talos/Hetzner fixture the provisioner's
// render-equals-create test also uses.
const renderFixtureSource = "../../../svc/provisioner/cluster/talos/testdata/render-hetzner"

// setupRenderProject copies the Talos/Hetzner fixture into a temporary
// project directory and changes into it.
func setupRenderProject(t *testing.T) string {
	t.Helper()

	source, err := filepath.Abs(renderFixtureSource)
	require.NoError(t, err)

	project := t.TempDir()
	require.NoError(t, os.CopyFS(project, os.DirFS(source)))
	t.Chdir(project)

	return project
}

// readRendered reads a file the render wrote under the test's temporary project.
func readRendered(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir().
	require.NoError(t, err)

	return data
}

func runRender(t *testing.T, enabled bool, args ...string) error {
	t.Helper()

	cmd := cluster.NewRenderCmd()
	if enabled {
		cmd.Flags().Bool(flags.ExperimentalFlagName, true, "")
	}

	cmd.SetArgs(args)
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	//nolint:wrapcheck // the test asserts the command's own error.
	return cmd.Execute()
}

//nolint:paralleltest // changes the working directory.
func TestRenderIsOffByDefault(t *testing.T) {
	project := setupRenderProject(t)

	err := runRender(t, false, "out")
	require.ErrorIs(t, err, experimental.ErrDisabled)
	assert.NoDirExists(t, filepath.Join(project, "out"))
}

func TestRenderWritesOneFilePerRoleWithoutCredentials(t *testing.T) {
	t.Setenv(v1alpha1.DefaultHetznerTokenEnvVar, "")

	project := setupRenderProject(t)

	err := runRender(t, true, "out", "--endpoint", "203.0.113.10")
	require.NoError(t, err)

	controlPlane := readRendered(t, filepath.Join(project, "out", "controlplane.yaml"))
	worker := readRendered(t, filepath.Join(project, "out", "worker.yaml"))

	assert.Contains(t, string(controlPlane), "hostname: prod-control-plane-1")
	assert.Contains(t, string(controlPlane), "https://203.0.113.10:6443")
	assert.Contains(t, string(controlPlane), talosprovisioner.RenderPlaceholderHcloudToken)
	assert.NotContains(t, string(controlPlane), "extraKernelArgs")
	assert.Contains(t, string(worker), "hostname: prod-worker-1")

	info, err := os.Stat(filepath.Join(project, "out", "controlplane.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

//nolint:paralleltest // changes the working directory.
func TestRenderWithSuppliedSecretsIsReproducible(t *testing.T) {
	project := setupRenderProject(t)

	bundle, err := secrets.NewBundle(secrets.NewClock(), nil)
	require.NoError(t, err)

	encoded, err := yaml.Marshal(bundle)
	require.NoError(t, err)

	secretsPath := filepath.Join(project, "secrets.yaml")
	require.NoError(t, os.WriteFile(secretsPath, encoded, 0o600))

	for _, dir := range []string{"first", "second"} {
		require.NoError(t, runRender(t, true, dir, "--secrets", secretsPath))
	}

	for _, file := range []string{"controlplane.yaml", "worker.yaml"} {
		first := readRendered(t, filepath.Join(project, "first", file))
		second := readRendered(t, filepath.Join(project, "second", file))

		assert.True(t, bytes.Equal(first, second), "%s differs between renders", file)
		assert.Contains(t, string(first), bundle.Secrets.BootstrapToken)
	}
}

//nolint:paralleltest // changes the working directory.
func TestRenderReplacesOutputSymlinkWithoutFollowingIt(t *testing.T) {
	project := setupRenderProject(t)

	input := filepath.Join(project, "ksail.yaml")
	before := readRendered(t, input)

	require.NoError(t, os.Mkdir(filepath.Join(project, "out"), 0o750))
	require.NoError(t, os.Symlink(input, filepath.Join(project, "out", "worker.yaml")))

	require.NoError(t, runRender(t, true, "out"))

	assert.Equal(
		t,
		string(before),
		string(readRendered(t, input)),
		"symlink target was overwritten",
	)

	info, err := os.Lstat(filepath.Join(project, "out", "worker.yaml"))
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular(), "worker.yaml is still a symlink")
	assert.Contains(t, string(readRendered(t, filepath.Join(project, "out", "worker.yaml"))),
		"hostname: prod-worker-1")
}

//nolint:paralleltest // changes the working directory.
func TestRenderMakesExistingOutputFilesOwnerOnly(t *testing.T) {
	project := setupRenderProject(t)

	out := filepath.Join(project, "out")
	require.NoError(t, os.Mkdir(out, 0o750))

	for _, file := range []string{"controlplane.yaml", "worker.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(out, file), []byte("old"), 0o600))
		// The wide mode is the one the render must replace.
		err := os.Chmod(filepath.Join(out, file), 0o644) //nolint:gosec // see above.
		require.NoError(t, err)
	}

	require.NoError(t, runRender(t, true, "out"))

	for _, file := range []string{"controlplane.yaml", "worker.yaml"} {
		info, err := os.Stat(filepath.Join(out, file))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), file)
	}

	entries, err := os.ReadDir(out)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "temporary files were left behind")
}
