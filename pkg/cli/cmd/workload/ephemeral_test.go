package workload_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sentinel errors for the fake provisioner and its callers, per err113 (no
// dynamic errors.New at use-site).
var (
	errEphemeralFnFailed     = errors.New("fn failed")
	errEphemeralCreateFailed = errors.New("create failed")
	errEphemeralDeleteFailed = errors.New("delete failed")
)

// fakeEphemeralProvisioner is a clusterprovisioner.Provisioner test double
// that records Create/Delete calls and can be configured to fail either.
type fakeEphemeralProvisioner struct {
	createErr error
	deleteErr error

	created []string
	deleted []string
}

func (f *fakeEphemeralProvisioner) Create(_ context.Context, name string) error {
	f.created = append(f.created, name)

	return f.createErr
}

func (f *fakeEphemeralProvisioner) Delete(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)

	return f.deleteErr
}

func (f *fakeEphemeralProvisioner) Start(_ context.Context, _ string) error { return nil }
func (f *fakeEphemeralProvisioner) Stop(_ context.Context, _ string) error  { return nil }

func (f *fakeEphemeralProvisioner) List(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeEphemeralProvisioner) Exists(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func installEphemeralProvisioner(t *testing.T, fake *fakeEphemeralProvisioner) {
	t.Helper()

	restore := workload.ExportSetEphemeralProvisioner(
		func(_ string) clusterprovisioner.Provisioner { return fake },
	)
	t.Cleanup(restore)
}

// newTestCommand returns a minimal *cobra.Command with a background context,
// suitable for exercising withEphemeralCluster without a real CLI invocation.
func newTestCommand(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(context.Background())

	return cmd
}

func newEphemeralTestCommand(t *testing.T, fake *fakeEphemeralProvisioner) *cobra.Command {
	t.Helper()

	installEphemeralProvisioner(t, fake)

	return newTestCommand(t)
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterProvisionsAndTearsDownOnSuccess(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	ran := false
	cmd := newEphemeralTestCommand(t, fake)

	err := workload.ExportWithEphemeralCluster(cmd.Context(), cmd, func(_ context.Context) error {
		ran = true
		// The cluster must be created before fn runs, and not yet deleted.
		assert.Len(t, fake.created, 1)
		assert.Empty(t, fake.deleted)

		return nil
	})

	require.NoError(t, err)
	assert.True(t, ran)
	assert.Len(t, fake.created, 1)
	assert.Len(t, fake.deleted, 1)
	assert.Equal(
		t,
		fake.created[0],
		fake.deleted[0],
		"the same cluster name must be created and deleted",
	)
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterTearsDownEvenWhenFnFails(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	cmd := newEphemeralTestCommand(t, fake)

	err := workload.ExportWithEphemeralCluster(cmd.Context(), cmd, func(_ context.Context) error {
		return errEphemeralFnFailed
	})

	require.ErrorIs(t, err, errEphemeralFnFailed)
	assert.Len(t, fake.created, 1)
	assert.Len(t, fake.deleted, 1, "teardown must run even when fn fails")
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterSkipsFnAndTearsDownWhenCreateFails(t *testing.T) {
	fake := &fakeEphemeralProvisioner{createErr: errEphemeralCreateFailed}
	ran := false
	cmd := newEphemeralTestCommand(t, fake)

	err := workload.ExportWithEphemeralCluster(cmd.Context(), cmd, func(_ context.Context) error {
		ran = true

		return nil
	})

	require.ErrorIs(t, err, errEphemeralCreateFailed)
	assert.False(t, ran, "fn must not run when the ephemeral cluster fails to provision")
	require.Len(t, fake.deleted, 1, "teardown must run after a failed create to clean partial state")
	assert.Equal(t, fake.created[0], fake.deleted[0], "failed create cleanup must target the same cluster")
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterJoinsFnAndDeleteErrors(t *testing.T) {
	fake := &fakeEphemeralProvisioner{deleteErr: errEphemeralDeleteFailed}
	cmd := newEphemeralTestCommand(t, fake)

	err := workload.ExportWithEphemeralCluster(cmd.Context(), cmd, func(_ context.Context) error {
		return errEphemeralFnFailed
	})

	require.Error(t, err)
	require.ErrorIs(
		t, err, errEphemeralFnFailed, "fn's error must not be dropped when delete also fails",
	)
	require.ErrorIs(
		t, err, errEphemeralDeleteFailed, "delete's error must not be dropped when fn also fails",
	)
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterReturnsDeleteErrorWhenFnSucceeds(t *testing.T) {
	fake := &fakeEphemeralProvisioner{deleteErr: errEphemeralDeleteFailed}
	cmd := newEphemeralTestCommand(t, fake)

	err := workload.ExportWithEphemeralCluster(cmd.Context(), cmd, func(_ context.Context) error {
		return nil
	})

	require.ErrorIs(t, err, errEphemeralDeleteFailed)
}

//nolint:paralleltest // registers no shared state, but grouped with the seam-dependent tests above for readability.
func TestValidateCmdRegistersEphemeralFlagDefaultOff(t *testing.T) {
	flag := workload.NewValidateCmd().Flags().Lookup("ephemeral")
	require.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

//nolint:paralleltest // registers no shared state, but grouped with the seam-dependent tests above for readability.
func TestScanCmdRegistersEphemeralFlagDefaultOff(t *testing.T) {
	flag := workload.NewScanCmd().Flags().Lookup("ephemeral")
	require.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

// TestValidateEphemeralFlagProvisionsAndTearsDownCluster is an end-to-end
// check (through NewValidateCmd/Execute, not the seam directly) that setting
// --ephemeral actually routes through withEphemeralCluster with the real flag
// plumbing, using a fake provisioner so no Docker/KWOK dependency is needed.
// Validation itself runs unchanged against an empty directory (no manifests
// to fail on) — this test is about the ephemeral wiring, not validation logic.
//
//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestValidateEphemeralFlagProvisionsAndTearsDownCluster(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	installEphemeralProvisioner(t, fake)

	dir := t.TempDir()

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{dir, "--ephemeral"})

	var output bytes.Buffer

	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Len(t, fake.created, 1, "validate --ephemeral must provision a cluster")
	assert.Len(t, fake.deleted, 1, "validate --ephemeral must tear the cluster down")
}
