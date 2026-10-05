package cluster_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errAuditInventoryUnavailable = errors.New("provider inventory unavailable")

// auditedUpdateFake models a live configuration that converges on the first
// apply while a removed pool's server remains in the provider inventory.
type auditedUpdateFake struct {
	updatableUpgraderFake

	failedChanges []clusterupdate.Change
	auditErr      error
	updateCalls   int
	auditCalls    int
}

func (f *auditedUpdateFake) Update(
	context.Context, string, *v1alpha1.ClusterSpec, *v1alpha1.ClusterSpec,
	clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	f.updateCalls++
	f.diff = nil

	result := clusterupdate.NewEmptyUpdateResult()
	result.FailedChanges = append(result.FailedChanges, f.failedChanges...)

	return result, nil
}

func (f *auditedUpdateFake) AuditUpdate(
	_ context.Context,
	_ string,
	result *clusterupdate.UpdateResult,
) error {
	f.auditCalls++
	result.FailedChanges = append(result.FailedChanges, f.failedChanges...)

	return f.auditErr
}

type auditUpdateFactory struct {
	provisioner clusterprovisioner.Provisioner
}

func (f auditUpdateFactory) Create(
	context.Context, *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return f.provisioner, nil, nil
}

// prepareAuditUpdateCommand keeps one HOME, desired config and stateful provider
// across multiple fresh command invocations, as consecutive shell commands do.
func prepareAuditUpdateCommand(t *testing.T, provisioner clusterprovisioner.Provisioner) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)

	t.Cleanup(cluster.SetProvisionerFactoryForTests(auditUpdateFactory{provisioner}))
	t.Cleanup(cluster.ExportSetUpdateUnmanagedGuard(
		func(context.Context, *lifecycle.ResolvedClusterInfo) error { return nil },
	))
}

func executeAuditUpdateCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()

	stdoutPath, stderrPath := redirectProcessOutput(t)
	cmd := cluster.NewUpdateCmd()
	cmd.SetContext(t.Context())
	cmd.SetArgs(args)

	execErr := cmd.Execute()

	stdout, err := os.ReadFile(stdoutPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	stderr, err := os.ReadFile(stderrPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	output := string(stdout) + "\n" + string(stderr)
	if execErr != nil {
		return output, fmt.Errorf("execute update command: %w", execErr)
	}

	return output, nil
}

// The first apply may refresh Helm/Secret state before it reports the leftover.
// Once DiffConfig becomes empty, the second real command must still fail and
// report that server without invoking the mutation path again.
//
//nolint:paralleltest // changes HOME, working directory and process output.
func TestUpdateCommandReportsLeftoverServerAfterConfigConverges(t *testing.T) {
	provisioner := &auditedUpdateFake{
		updatableUpgraderFake: updatableUpgraderFake{
			diff: &clusterupdate.UpdateResult{
				InPlaceChanges: []clusterupdate.Change{{
					Field:    "talos.workers",
					OldValue: "1",
					NewValue: "2",
					Category: clusterupdate.ChangeCategoryInPlace,
				}},
			},
		},
		failedChanges: []clusterupdate.Change{{
			Field:  "worker.as-removed-1",
			Reason: "as-removed-1 belongs to unconfigured pool removed-pool; left untouched",
		}},
	}
	prepareAuditUpdateCommand(t, provisioner)

	firstOutput, firstErr := executeAuditUpdateCommand(t, "--yes")
	require.Error(t, firstErr)
	assert.Contains(t, firstOutput, "as-removed-1")
	assert.Contains(t, firstOutput, "removed-pool")

	secondOutput, secondErr := executeAuditUpdateCommand(t, "--yes")
	require.Error(t, secondErr, "an empty config diff must not hide the leftover server")
	assert.Contains(t, secondOutput, "as-removed-1")
	assert.Contains(t, secondOutput, "removed-pool")
	assert.NotContains(t, secondOutput, "No changes detected")
	assert.Equal(t, 1, provisioner.updateCalls, "the second command must not mutate")
	assert.Equal(t, 1, provisioner.auditCalls, "the second command must inspect inventory")
}

//nolint:paralleltest // changes HOME, working directory and process output.
func TestUpdateCommandNoChangeAuditReportsInventoryFailure(t *testing.T) {
	provisioner := &auditedUpdateFake{auditErr: errAuditInventoryUnavailable}
	prepareAuditUpdateCommand(t, provisioner)

	output, err := executeAuditUpdateCommand(t, "--yes")
	require.ErrorContains(t, err, "provider inventory unavailable")
	assert.NotContains(t, output, "No changes detected")
	assert.Zero(t, provisioner.updateCalls)
	assert.Equal(t, 1, provisioner.auditCalls)
}

//nolint:paralleltest // changes HOME, working directory and process output.
func TestUpdateCommandNoChangeAuditAcceptsCleanInventory(t *testing.T) {
	provisioner := &auditedUpdateFake{}
	prepareAuditUpdateCommand(t, provisioner)

	output, err := executeAuditUpdateCommand(t, "--yes")
	require.NoError(t, err)
	assert.Contains(t, output, "No changes detected")
	assert.Zero(t, provisioner.updateCalls)
	assert.Equal(t, 1, provisioner.auditCalls)
}

//nolint:paralleltest // changes HOME, working directory and process output.
func TestUpdateCommandDryRunDoesNotAuditProviderInventory(t *testing.T) {
	provisioner := &auditedUpdateFake{auditErr: errAuditInventoryUnavailable}
	prepareAuditUpdateCommand(t, provisioner)

	_, err := executeAuditUpdateCommand(t, "--dry-run")
	require.NoError(t, err)
	assert.Zero(t, provisioner.updateCalls)
	assert.Zero(t, provisioner.auditCalls)
}
