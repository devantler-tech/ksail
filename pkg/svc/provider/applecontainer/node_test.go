package applecontainer_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/applecontainer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const labelFlags = "--label ksail.io/managed-by=ksail --label ksail.io/cluster=dev " +
	"--label ksail.io/role=worker"

func workerSpec() applecontainer.NodeSpec {
	return applecontainer.NodeSpec{
		Name:           "dev-worker-2",
		ClusterName:    "dev",
		Role:           applecontainer.RoleWorker,
		Image:          "ghcr.io/siderolabs/talos:v1.14.2",
		CPUs:           4,
		MemoryMiB:      6144,
		Privileged:     true,
		ReadOnlyRootFS: true,
		Env:            map[string]string{"PLATFORM": "container", "A": "1"},
		Tmpfs:          []string{"/run", "/tmp"},
		Volumes: []applecontainer.VolumeMount{
			{Name: "state", Destination: "/system/state"},
			{Name: "var", Destination: "/var", Size: "10G"},
		},
	}
}

func TestCreateNode(t *testing.T) {
	t.Parallel()

	cli := newFakeCLI()

	require.NoError(
		t,
		applecontainer.NewProvider(cli).CreateNode(context.Background(), workerSpec()),
	)
	assert.Equal(t, []string{
		"volume create " + labelFlags + " dev-worker-2-state",
		"volume create " + labelFlags + " -s 10G dev-worker-2-var",
		"run --detach --name dev-worker-2 " + labelFlags +
			" --cap-add ALL --read-only-path NONE --masked-path NONE --read-only" +
			" --cpus 4 --memory 6144M --env A=1 --env PLATFORM=container" +
			" --tmpfs /run --tmpfs /tmp" +
			" --volume dev-worker-2-state:/system/state --volume dev-worker-2-var:/var" +
			" ghcr.io/siderolabs/talos:v1.14.2",
	}, cli.mutations())
}

func TestCreateNode_MinimalSpecOmitsOptionalFlags(t *testing.T) {
	t.Parallel()

	cli := newFakeCLI()
	spec := applecontainer.NodeSpec{
		Name: "dev-worker-2", ClusterName: "dev", Role: applecontainer.RoleWorker, Image: "img:1",
	}

	require.NoError(t, applecontainer.NewProvider(cli).CreateNode(context.Background(), spec))
	assert.Equal(t,
		[]string{"run --detach --name dev-worker-2 " + labelFlags + " img:1"}, cli.mutations())
}

func TestCreateNode_RefusesExistingName(t *testing.T) {
	t.Parallel()

	// The name is taken by a container this provider does not own; nothing may be created, and
	// above all nothing may be deleted.
	for _, name := range []string{"buildkit", "dev-worker-1"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cli := newFakeCLI()
			spec := workerSpec()
			spec.Name = name

			err := applecontainer.NewProvider(cli).CreateNode(context.Background(), spec)

			require.ErrorIs(t, err, applecontainer.ErrNodeExists)
			assert.Empty(t, cli.mutations())
		})
	}
}

func TestCreateNode_CleansUpAfterFailure(t *testing.T) {
	t.Parallel()

	t.Run("RunFails", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.failPrefix = "run"

		// A cancelled caller context must not stop the cleanup.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := applecontainer.NewProvider(cli).CreateNode(ctx, workerSpec())

		require.ErrorIs(t, err, errFake)

		mutations := cli.mutations()
		require.Len(t, mutations, 5)
		assert.Equal(t, "delete --force -- dev-worker-2", mutations[3])
		assert.Equal(t, "volume delete -- dev-worker-2-state dev-worker-2-var", mutations[4])
	})

	t.Run("SecondVolumeFails", func(t *testing.T) {
		t.Parallel()

		cli := newFakeCLI()
		cli.failPrefix = "volume create " + labelFlags + " -s"

		err := applecontainer.NewProvider(cli).CreateNode(context.Background(), workerSpec())

		require.ErrorIs(t, err, errFake)
		// Only the volume this call created is removed, and no container is touched.
		assert.Equal(t, []string{
			"volume create " + labelFlags + " dev-worker-2-state",
			"volume create " + labelFlags + " -s 10G dev-worker-2-var",
			"volume delete -- dev-worker-2-state",
		}, cli.mutations())
	})
}

func TestCreateNode_RejectsInvalidSpec(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*applecontainer.NodeSpec){
		"EmptyName":           func(s *applecontainer.NodeSpec) { s.Name = "" },
		"NameLooksLikeFlag":   func(s *applecontainer.NodeSpec) { s.Name = "--all" },
		"NameWithSpace":       func(s *applecontainer.NodeSpec) { s.Name = "a b" },
		"EmptyCluster":        func(s *applecontainer.NodeSpec) { s.ClusterName = "" },
		"ClusterWithEquals":   func(s *applecontainer.NodeSpec) { s.ClusterName = "a=b" },
		"UnknownRole":         func(s *applecontainer.NodeSpec) { s.Role = "etcd" },
		"EmptyImage":          func(s *applecontainer.NodeSpec) { s.Image = "" },
		"ImageLooksLikeFlag":  func(s *applecontainer.NodeSpec) { s.Image = "--rm" },
		"NegativeCPUs":        func(s *applecontainer.NodeSpec) { s.CPUs = -1 },
		"NegativeMemory":      func(s *applecontainer.NodeSpec) { s.MemoryMiB = -1 },
		"VolumeNameWithSlash": func(s *applecontainer.NodeSpec) { s.Volumes[0].Name = "a/b" },
		"VolumeNoDestination": func(s *applecontainer.NodeSpec) { s.Volumes[0].Destination = "" },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cli := newFakeCLI()
			spec := workerSpec()
			mutate(&spec)

			err := applecontainer.NewProvider(cli).CreateNode(context.Background(), spec)

			require.ErrorIs(t, err, applecontainer.ErrInvalidNodeSpec)
			assert.Empty(t, cli.allCalls(), "an invalid spec must not reach the CLI")
		})
	}
}

// writeScript writes an executable stand-in for the `container` CLI without
// leaving this process with a writable descriptor that a concurrent fork can
// inherit.
func writeScript(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "container")
	require.NoError(t, writeExecutableScriptFile(
		t.Context(), path, "#!/bin/sh\n"+body+"\n",
	))

	return path
}

func writeExecutableScriptFile(ctx context.Context, path, content string) error {
	// #nosec G204 -- constant shell program; test-owned path is positional-only.
	command := exec.CommandContext(
		ctx, "sh", "-c", `umask 077 && cat >"$1" && chmod 0700 "$1"`, "sh", path,
	)
	command.Stdin = strings.NewReader(content)

	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("write executable script %s: %w (%s)", path, err, output)
	}

	return nil
}

func TestWriteExecutableScriptFile_ProducesRunnableOwnerOnlyScript(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "container")
	content := "#!/bin/sh\nprintf 'ready %s\\n' \"$1\"\n"

	require.NoError(t, writeExecutableScriptFile(t.Context(), path, content))

	written, err := os.ReadFile(path) //nolint:gosec // Test-owned temporary path.
	require.NoError(t, err)
	assert.Equal(t, content, string(written))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	//nolint:gosec // path is a test-owned temporary executable.
	output, err := exec.CommandContext(t.Context(), path, "now").CombinedOutput()
	require.NoError(t, err, "script must execute immediately: %s", output)
	assert.Equal(t, "ready now\n", string(output))
}

func TestWriteExecutableScriptFile_AvoidsTextFileBusy(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("ETXTBSY on exec of a file open for writing is Linux semantics")
	}

	const content = "#!/bin/sh\nexit 0\n"

	heldPath := filepath.Join(t.TempDir(), "held-open")
	require.NoError(t, os.WriteFile(heldPath, []byte(content), 0o600))
	//nolint:gosec // Owner execute is required for this test-owned temporary script.
	require.NoError(t, os.Chmod(heldPath, 0o700))

	// This is the failure mechanism from the coverage run: Linux refuses to
	// execute a test stub while this process owns a writable descriptor for it.
	//nolint:gosec // heldPath is a test-owned temporary executable.
	writer, err := os.OpenFile(heldPath, os.O_WRONLY, 0)
	require.NoError(t, err)

	//nolint:gosec // heldPath is a test-owned temporary executable.
	output, err := exec.CommandContext(t.Context(), heldPath).CombinedOutput()
	require.ErrorIs(t, err, syscall.ETXTBSY, "exec must expose the exact failure: %s", output)
	require.NoError(t, writer.Close())

	// The writer used by writeScript must leave no writable descriptor in this
	// process for a concurrent fork to inherit.
	safePath := filepath.Join(t.TempDir(), "fork-safe")
	require.NoError(t, writeExecutableScriptFile(t.Context(), safePath, content))

	//nolint:gosec // safePath is a test-owned temporary executable.
	output, err = exec.CommandContext(t.Context(), safePath).CombinedOutput()
	require.NoError(t, err, "fork-safe stub must execute immediately: %s", output)
}

func TestExecRunner(t *testing.T) {
	t.Parallel()

	t.Run("ReturnsStdout", func(t *testing.T) {
		t.Parallel()

		runner := applecontainer.NewExecRunner(writeScript(t, `echo "$1-$2"; echo noise >&2`))

		output, err := runner.Run(context.Background(), "volume", "list")

		require.NoError(t, err)
		assert.Equal(t, "volume-list\n", string(output))
	})

	t.Run("MissingBinary", func(t *testing.T) {
		t.Parallel()

		runner := applecontainer.NewExecRunner(filepath.Join(t.TempDir(), "absent"))

		_, err := runner.Run(context.Background(), "system", "status")

		require.ErrorIs(t, err, applecontainer.ErrCLINotFound)
	})

	t.Run("FailureCarriesSubcommandAndStderrButNotArguments", func(t *testing.T) {
		t.Parallel()

		runner := applecontainer.NewExecRunner(writeScript(t, `echo "Error: boom" >&2; exit 3`))

		_, err := runner.Run(context.Background(), "run", "--env", "TOKEN=hunter2", "img")

		require.ErrorIs(t, err, applecontainer.ErrCommandFailed)
		assert.Contains(t, err.Error(), "run: ")
		assert.Contains(t, err.Error(), "Error: boom")
		assert.NotContains(t, err.Error(), "hunter2")
	})

	t.Run("MissingBinaryMakesProviderUnavailable", func(t *testing.T) {
		t.Parallel()

		prov := applecontainer.NewProvider(
			applecontainer.NewExecRunner(filepath.Join(t.TempDir(), "absent")))

		assert.False(t, prov.IsAvailable())
		require.ErrorIs(t, prov.CheckAvailable(context.Background()), applecontainer.ErrCLINotFound)
	})
}
