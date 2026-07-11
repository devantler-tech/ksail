package workload_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sentinel errors for the fake provisioner and its callers, per err113 (no
// dynamic errors.New at use-site).
var (
	errEphemeralFnFailed      = errors.New("fn failed")
	errEphemeralCreateFailed  = errors.New("create failed")
	errEphemeralDeleteFailed  = errors.New("delete failed")
	errEphemeralWaitFailed    = errors.New("wait failed")
	errEphemeralCleanupFailed = errors.New("cleanup failed")
	errEphemeralBackendFailed = errors.New("backend failed")
)

// fakeEphemeralProvisioner is a clusterprovisioner.Provisioner test double
// that records Create/Delete calls and can be configured to fail either.
type fakeEphemeralProvisioner struct {
	createErr error
	deleteErr error
	events    *[]string

	created []string
	deleted []string
}

// Create records the requested cluster name and returns the configured
// creation error.
func (f *fakeEphemeralProvisioner) Create(_ context.Context, name string) error {
	f.created = append(f.created, name)
	if f.events != nil {
		*f.events = append(*f.events, "create")
	}

	return f.createErr
}

// Delete records the requested cluster name and returns the configured
// deletion error.
func (f *fakeEphemeralProvisioner) Delete(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	if f.events != nil {
		*f.events = append(*f.events, "delete")
	}

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

// fakeEphemeralWaiter records the readiness-wait calls withEphemeralCluster
// makes and can be configured to fail, standing in for the real
// k8s.WaitForClusterReady (which would poll a cluster that does not exist
// under the fake provisioner).
type fakeEphemeralWaiter struct {
	waitErr error
	events  *[]string

	kubeconfigPaths []string
	contexts        []string
}

// wait records the kubeconfig and context supplied to the readiness gate and
// returns the configured wait error.
func (f *fakeEphemeralWaiter) wait(_ context.Context, kubeconfigPath, contextName string) error {
	f.kubeconfigPaths = append(f.kubeconfigPaths, kubeconfigPath)

	f.contexts = append(f.contexts, contextName)
	if f.events != nil {
		*f.events = append(*f.events, "ready")
	}

	return f.waitErr
}

// fakeEphemeralBackend records the local workspace cleanup owned by the
// backend, separately from cluster deletion owned by the provisioner.
type fakeEphemeralBackend struct {
	workspace  string
	cleanupErr error
	events     *[]string
	cleaned    int
}

func installEphemeralWaiter(t *testing.T, fake *fakeEphemeralWaiter) {
	t.Helper()

	restore := workload.ExportSetEphemeralClusterWaiter(fake.wait)
	t.Cleanup(restore)
}

// installEphemeralProvisioner wires the fake provisioner and the supplied
// readiness waiter. A nil waiter selects a no-op fake because there is never a
// real cluster to poll under the fake provisioner.
func installEphemeralProvisioner(
	t *testing.T,
	fake *fakeEphemeralProvisioner,
	waiter *fakeEphemeralWaiter,
) *fakeEphemeralBackend {
	t.Helper()

	if waiter == nil {
		waiter = &fakeEphemeralWaiter{}
	}

	backend := &fakeEphemeralBackend{}
	restore := workload.ExportSetEphemeralBackendFactory(
		func(name string) (workload.ExportEphemeralBackend, error) {
			backend.workspace = filepath.Join(t.TempDir(), "ephemeral-backend")
			require.NoError(t, os.MkdirAll(backend.workspace, 0o750))

			return workload.ExportEphemeralBackend{
				Provisioner: fake,
				Cluster: workload.EphemeralCluster{
					Name:           name,
					KubeconfigPath: filepath.Join(backend.workspace, "kubeconfig"),
					Context:        "kind-" + name,
				},
				Cleanup: func() error {
					backend.cleaned++
					if backend.events != nil {
						*backend.events = append(*backend.events, "cleanup")
					}

					return errors.Join(os.RemoveAll(backend.workspace), backend.cleanupErr)
				},
			}, nil
		},
	)
	t.Cleanup(restore)

	installEphemeralWaiter(t, waiter)

	return backend
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

	installEphemeralProvisioner(t, fake, nil)

	return newTestCommand(t)
}

// TestCreateEphemeralBackendSelectsKindAndIsolatesKubeconfig pins the real
// backend factory independently from the orchestration seam used by the
// remaining tests. Constructing the provisioner does not create a cluster.
func TestCreateEphemeralBackendSelectsKindAndIsolatesKubeconfig(t *testing.T) {
	t.Parallel()

	const name = "ksail-ephemeral-factory-test"

	backend, err := workload.ExportCreateEphemeralBackend(name)
	require.NoError(t, err)

	assert.IsType(t, &kindprovisioner.Provisioner{}, backend.Provisioner)
	assert.Equal(t, name, backend.Cluster.Name)
	assert.Equal(t, "kind-"+name, backend.Cluster.Context)
	assert.Equal(t, "kubeconfig", filepath.Base(backend.Cluster.KubeconfigPath))

	workspace := filepath.Dir(backend.Cluster.KubeconfigPath)
	require.DirExists(t, workspace)
	require.NoError(t, backend.Cleanup())
	assert.NoDirExists(t, workspace)
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterProvisionsAndTearsDownOnSuccess(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	ran := false
	cmd := newTestCommand(t)
	backend := installEphemeralProvisioner(t, fake, nil)

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(_ context.Context, cluster workload.EphemeralCluster) error {
			ran = true
			// The cluster must be created before fn runs, and not yet deleted.
			assert.Len(t, fake.created, 1)
			assert.Empty(t, fake.deleted)
			// The connection handle must describe the created cluster per the
			// Kind context naming convention.
			assert.Equal(t, fake.created[0], cluster.Name)
			assert.Equal(t, "kind-"+fake.created[0], cluster.Context)
			assert.NotEmpty(t, cluster.KubeconfigPath)

			return nil
		},
	)

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
	assert.Equal(t, 1, backend.cleaned)
	assert.NoDirExists(t, backend.workspace)
}

// TestWithEphemeralClusterOrdersBackendLifecycle pins the full lifecycle,
// including local kubeconfig cleanup after cluster deletion.
//
//nolint:paralleltest // swaps shared backend and readiness seams.
func TestWithEphemeralClusterOrdersBackendLifecycle(t *testing.T) {
	events := []string{}
	fake := &fakeEphemeralProvisioner{events: &events}
	cmd := newTestCommand(t)
	waiter := &fakeEphemeralWaiter{events: &events}
	backend := installEphemeralProvisioner(t, fake, waiter)
	backend.events = &events

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(_ context.Context, _ workload.EphemeralCluster) error {
			events = append(events, "callback")

			return nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"create", "ready", "callback", "delete", "cleanup"}, events)
	assert.Equal(t, 1, backend.cleaned)
	assert.NoDirExists(t, backend.workspace)
}

// TestWithEphemeralClusterWaitsForReadinessBeforeFn pins that the readiness
// waiter is called with the same kubeconfig path and context handed to runFn,
// so the handle runFn receives is the one that was actually verified.
//
//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterWaitsForReadinessBeforeFn(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	cmd := newTestCommand(t)
	waiter := &fakeEphemeralWaiter{}
	installEphemeralProvisioner(t, fake, waiter)

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(_ context.Context, cluster workload.EphemeralCluster) error {
			require.Len(t, waiter.contexts, 1, "readiness must be verified before fn runs")
			assert.Equal(t, cluster.Context, waiter.contexts[0])
			assert.Equal(t, cluster.KubeconfigPath, waiter.kubeconfigPaths[0])

			return nil
		},
	)

	require.NoError(t, err)
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterSkipsFnAndTearsDownWhenWaitFails(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	ran := false
	cmd := newTestCommand(t)
	backend := installEphemeralProvisioner(
		t, fake, &fakeEphemeralWaiter{waitErr: errEphemeralWaitFailed},
	)

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(_ context.Context, _ workload.EphemeralCluster) error {
			ran = true

			return nil
		},
	)

	require.ErrorIs(t, err, errEphemeralWaitFailed)
	assert.False(t, ran, "fn must not run when the ephemeral cluster never becomes ready")
	assert.Len(t, fake.deleted, 1, "teardown must run when the readiness wait fails")
	assert.Equal(t, 1, backend.cleaned)
	assert.NoDirExists(t, backend.workspace)
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterTearsDownEvenWhenFnFails(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	cmd := newTestCommand(t)
	backend := installEphemeralProvisioner(t, fake, nil)

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(_ context.Context, _ workload.EphemeralCluster) error {
			return errEphemeralFnFailed
		},
	)

	require.ErrorIs(t, err, errEphemeralFnFailed)
	assert.Len(t, fake.created, 1)
	assert.Len(t, fake.deleted, 1, "teardown must run even when fn fails")
	assert.Equal(t, 1, backend.cleaned)
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterSkipsFnAndTearsDownWhenCreateFails(t *testing.T) {
	fake := &fakeEphemeralProvisioner{createErr: errEphemeralCreateFailed}
	ran := false
	cmd := newTestCommand(t)
	backend := installEphemeralProvisioner(t, fake, nil)

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(_ context.Context, _ workload.EphemeralCluster) error {
			ran = true

			return nil
		},
	)

	require.ErrorIs(t, err, errEphemeralCreateFailed)
	assert.False(t, ran, "fn must not run when the ephemeral cluster fails to provision")
	require.Len(
		t,
		fake.deleted,
		1,
		"teardown must run after a failed create to clean partial state",
	)
	assert.Equal(
		t,
		fake.created[0],
		fake.deleted[0],
		"failed create cleanup must target the same cluster",
	)
	assert.Equal(t, 1, backend.cleaned)
	assert.NoDirExists(t, backend.workspace)
}

// TestWithEphemeralClusterSuppressesNotFoundCleanupAfterCreateFailure verifies
// a failed create reports its real cause without adding the expected absence
// of a partially-created cluster as a second failure.
//
//nolint:paralleltest // swaps shared backend and readiness seams.
func TestWithEphemeralClusterSuppressesNotFoundCleanupAfterCreateFailure(t *testing.T) {
	fake := &fakeEphemeralProvisioner{
		createErr: errEphemeralCreateFailed,
		deleteErr: clustererr.ErrClusterNotFound,
	}
	cmd := newTestCommand(t)
	installEphemeralProvisioner(t, fake, nil)

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(context.Context, workload.EphemeralCluster) error {
			t.Fatal("callback must not run after create failure")

			return nil
		},
	)

	require.ErrorIs(t, err, errEphemeralCreateFailed)
	assert.NotErrorIs(t, err, clustererr.ErrClusterNotFound)
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterJoinsFnAndDeleteErrors(t *testing.T) {
	fake := &fakeEphemeralProvisioner{deleteErr: errEphemeralDeleteFailed}
	cmd := newEphemeralTestCommand(t, fake)

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(_ context.Context, _ workload.EphemeralCluster) error {
			return errEphemeralFnFailed
		},
	)

	require.Error(t, err)
	require.ErrorIs(
		t, err, errEphemeralFnFailed, "fn's error must not be dropped when delete also fails",
	)
	require.ErrorIs(
		t, err, errEphemeralDeleteFailed, "delete's error must not be dropped when fn also fails",
	)
}

// TestWithEphemeralClusterJoinsFnDeleteAndCleanupErrors verifies all three
// independent lifecycle failures remain inspectable in the returned error.
//
//nolint:paralleltest // swaps shared backend and readiness seams.
func TestWithEphemeralClusterJoinsFnDeleteAndCleanupErrors(t *testing.T) {
	fake := &fakeEphemeralProvisioner{deleteErr: errEphemeralDeleteFailed}
	cmd := newTestCommand(t)
	backend := installEphemeralProvisioner(t, fake, nil)
	backend.cleanupErr = errEphemeralCleanupFailed

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(_ context.Context, _ workload.EphemeralCluster) error {
			return errEphemeralFnFailed
		},
	)

	require.ErrorIs(t, err, errEphemeralFnFailed)
	require.ErrorIs(t, err, errEphemeralDeleteFailed)
	require.ErrorIs(t, err, errEphemeralCleanupFailed)
	assert.Equal(t, 1, backend.cleaned)
}

// TestWithEphemeralClusterCleansUpAfterCancellation verifies a cancelled run
// still deletes the cluster and removes the local backend workspace.
//
//nolint:paralleltest // swaps shared backend and readiness seams.
func TestWithEphemeralClusterCleansUpAfterCancellation(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	cmd := newTestCommand(t)
	backend := installEphemeralProvisioner(t, fake, nil)
	ctx, cancel := context.WithCancel(cmd.Context())
	cancel()

	err := workload.ExportWithEphemeralCluster(
		ctx, cmd,
		func(ctx context.Context, _ workload.EphemeralCluster) error {
			return ctx.Err()
		},
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.Len(t, fake.deleted, 1)
	assert.Equal(t, 1, backend.cleaned)
	assert.NoDirExists(t, backend.workspace)
}

// TestWithEphemeralClusterStopsWhenBackendCreationFails verifies provisioning
// and the callback are never attempted without a usable backend.
//
//nolint:paralleltest // swaps shared backend seam.
func TestWithEphemeralClusterStopsWhenBackendCreationFails(t *testing.T) {
	restore := workload.ExportSetEphemeralBackendFactory(
		func(string) (workload.ExportEphemeralBackend, error) {
			return workload.ExportEphemeralBackend{}, errEphemeralBackendFailed
		},
	)
	t.Cleanup(restore)

	ran := false
	cmd := newTestCommand(t)
	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(context.Context, workload.EphemeralCluster) error {
			ran = true

			return nil
		},
	)

	require.ErrorIs(t, err, errEphemeralBackendFailed)
	assert.False(t, ran)
}

//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestWithEphemeralClusterReturnsDeleteErrorWhenFnSucceeds(t *testing.T) {
	fake := &fakeEphemeralProvisioner{deleteErr: errEphemeralDeleteFailed}
	cmd := newEphemeralTestCommand(t, fake)

	err := workload.ExportWithEphemeralCluster(
		cmd.Context(), cmd,
		func(_ context.Context, _ workload.EphemeralCluster) error {
			return nil
		},
	)

	require.ErrorIs(t, err, errEphemeralDeleteFailed)
}

//nolint:paralleltest // registers no shared state, but grouped with the seam-dependent tests above for readability.
func TestValidateCmdRegistersEphemeralFlagDefaultOff(t *testing.T) {
	flag := workload.NewValidateCmd().Flags().Lookup("ephemeral")
	require.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
	assert.Contains(t, flag.Usage, "Kind")
	assert.Contains(t, flag.Usage, "next slice")
	assert.NotContains(t, flag.Usage, "KWOK")
}

//nolint:paralleltest // registers no shared state, but grouped with the seam-dependent tests above for readability.
func TestScanCmdRegistersEphemeralFlagDefaultOff(t *testing.T) {
	flag := workload.NewScanCmd().Flags().Lookup("ephemeral")
	require.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
	assert.Contains(t, flag.Usage, "Kind")
	assert.Contains(t, flag.Usage, "next slice")
	assert.NotContains(t, flag.Usage, "KWOK")
}

// TestValidateEphemeralFlagProvisionsAndTearsDownCluster is an end-to-end
// check (through NewValidateCmd/Execute, not the seam directly) that setting
// --ephemeral actually routes through withEphemeralCluster with the real flag
// plumbing, using a fake backend so no Docker/Kind dependency is needed.
// Validation itself runs unchanged against an empty directory (no manifests
// to fail on) — this test is about the ephemeral wiring, not validation logic.
//
//nolint:paralleltest // swaps the shared package-level provisioner seam; cannot run in parallel.
func TestValidateEphemeralFlagProvisionsAndTearsDownCluster(t *testing.T) {
	fake := &fakeEphemeralProvisioner{}
	installEphemeralProvisioner(t, fake, nil)

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
