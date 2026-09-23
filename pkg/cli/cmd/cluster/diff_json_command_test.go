package cluster_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDiffCommand executes the real `ksail cluster diff` command — flag parsing,
// config loading, component detection, the drift checks and the provisioner
// diff — in a project whose kubeconfig does not exist, so component detection
// warns, and returns what reached stdout and stderr.
func runDiffCommand(t *testing.T, args ...string) (string, string) {
	t.Helper()

	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	require.NoError(t, os.Remove(filepath.Join(workingDir, "kubeconfig")))

	t.Cleanup(cluster.SetProvisionerFactoryForTests(
		updatableUpgraderFactory{&updatableUpgraderFake{}},
	))

	stdoutPath, stderrPath := redirectProcessOutput(t)

	cmd := cluster.NewDiffCmd()
	cmd.SetContext(t.Context())
	cmd.SetArgs(args)

	execErr := cmd.Execute()

	stdout, err := os.ReadFile(stdoutPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	stderr, err := os.ReadFile(stderrPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	require.NoError(t, execErr, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	return string(stdout), string(stderr)
}

// `ksail cluster diff --output json` is meant for CI and MCP consumers that
// parse stdout: it must carry exactly one JSON document, even when config
// loading reports progress and component detection warns.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestDiffCommandJSONLeavesOneDocumentOnStdout(t *testing.T) {
	stdout, stderr := runDiffCommand(t, "--output", "json")

	var decoded map[string]any

	require.NoError(t, json.Unmarshal([]byte(stdout), &decoded),
		"stdout must be exactly one JSON document: %q", stdout)
	assert.Contains(t, decoded, "totalChanges")
	assert.Contains(t, stderr, "config loaded")
	assert.Contains(t, stderr, "Cannot create Helm client for component detection")
}

// In text mode config loading still reports on stdout; warnings go to stderr.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestDiffCommandTextKeepsWarningsOnStderr(t *testing.T) {
	stdout, stderr := runDiffCommand(t)

	assert.Contains(t, stdout, "config loaded")
	assert.NotContains(t, stdout, "component detection")
	assert.Contains(t, stderr, "Cannot create Helm client for component detection")
}
