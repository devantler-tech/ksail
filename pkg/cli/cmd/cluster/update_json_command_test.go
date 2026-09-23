package cluster_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updatableUpgraderFake adds the Updater surface to versionUpgraderFake, so the
// whole `cluster update` command can run against it without a real cluster.
type updatableUpgraderFake struct {
	versionUpgraderFake
}

func (*updatableUpgraderFake) Update(
	context.Context, string, *v1alpha1.ClusterSpec, *v1alpha1.ClusterSpec,
	clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	return clusterupdate.NewEmptyUpdateResult(), nil
}

func (*updatableUpgraderFake) DiffConfig(
	context.Context, string, *v1alpha1.ClusterSpec, *v1alpha1.ClusterSpec,
) (*clusterupdate.UpdateResult, error) {
	return clusterupdate.NewEmptyUpdateResult(), nil
}

func (*updatableUpgraderFake) GetCurrentConfig(
	context.Context, string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	return &v1alpha1.ClusterSpec{}, nil, nil
}

type updatableUpgraderFactory struct{ provisioner *updatableUpgraderFake }

func (f updatableUpgraderFactory) Create(
	context.Context, *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return f.provisioner, nil, nil
}

// redirectProcessOutput points the process's stdout and stderr at files for the
// rest of the test and returns their paths. The update command is then built
// and run without SetOut/SetErr, so every writer resolves to the same streams a
// user's shell would see — including writers captured when the command is
// constructed, which a per-command buffer cannot observe.
func redirectProcessOutput(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	stdoutPath := filepath.Join(dir, "stdout")
	stderrPath := filepath.Join(dir, "stderr")

	stdoutFile, err := os.Create(stdoutPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	stderrFile, err := os.Create(stderrPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	origStdout, origStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutFile, stderrFile

	t.Cleanup(func() {
		os.Stdout, os.Stderr = origStdout, origStderr

		_ = stdoutFile.Close()
		_ = stderrFile.Close()
	})

	return stdoutPath, stderrPath
}

// runUpdateCommand executes the real `ksail cluster update` command — flag
// parsing, config loading, the managed-target guard, provisioner verification,
// version reconciliation, the diff, and the dry-run report — against an
// in-place upgrader fake whose distribution version is pinned one release ahead,
// and returns what reached stdout and stderr.
func runUpdateCommand(t *testing.T, args ...string) (string, string) {
	t.Helper()

	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)

	provisioner := &updatableUpgraderFake{versionUpgraderFake: versionUpgraderFake{
		current: clusterupdate.VersionInfo{
			KubernetesVersion:   "v1.34.0",
			DistributionVersion: "v1.12.0",
		},
		distributionPin: "v1.13.0",
	}}

	t.Cleanup(cluster.SetProvisionerFactoryForTests(updatableUpgraderFactory{provisioner}))
	t.Cleanup(cluster.ExportSetUpdateUnmanagedGuard(
		func(context.Context, *lifecycle.ResolvedClusterInfo) error { return nil },
	))

	stdoutPath, stderrPath := redirectProcessOutput(t)

	cmd := cluster.NewUpdateCmd()
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

// `ksail cluster update --dry-run --output json` is the preview the docs pipe
// into jq: the whole command must leave exactly one JSON document on stdout,
// with config loading and version reconciliation reported on stderr.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestUpdateCommandDryRunJSONLeavesOneDocumentOnStdout(t *testing.T) {
	stdout, stderr := runUpdateCommand(t, "--dry-run", "--output", "json")

	var decoded map[string]any

	require.NoError(t, json.Unmarshal([]byte(stdout), &decoded),
		"stdout must be exactly one JSON document: %q", stdout)
	assert.Contains(t, decoded, "totalChanges")
	assert.Contains(t, stderr, "config loaded")
	assert.Contains(t, stderr, "Would upgrade distribution to pinned version v1.13.0")
}

// The counterpart: in text mode the same run keeps reporting on stdout.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestUpdateCommandDryRunTextReportsOnStdout(t *testing.T) {
	stdout, stderr := runUpdateCommand(t, "--dry-run")

	assert.Contains(t, stdout, "config loaded")
	assert.Contains(t, stdout, "Would upgrade distribution to pinned version v1.13.0")
	assert.NotContains(t, stderr, "Would upgrade distribution")
}
