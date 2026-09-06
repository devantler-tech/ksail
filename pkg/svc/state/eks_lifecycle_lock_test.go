package state_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errLifecycleAction = errors.New("lifecycle action failed")

// TestEKSLifecycleLockCancelsBeforeStateMutation rejects cancellation before creating lock state.
func TestEKSLifecycleLockCancelsBeforeStateMutation(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("HOME", stateHome)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	called := false
	err := state.WithEKSLifecycleLock(ctx, "demo", func() error {
		called = true

		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, called)

	_, err = os.Stat(filepath.Join(stateHome, ".ksail"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestEKSLifecycleLockRejectsInvalidTarget rejects names that cannot identify safe state paths.
//
//nolint:paralleltest // subtests share the parent's temporary HOME.
func TestEKSLifecycleLockRejectsInvalidTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, name := range []string{"", " ", ".", "../demo", "a/b", `a\b`} {
		t.Run(name, func(t *testing.T) {
			called := false
			err := state.WithEKSLifecycleLock(t.Context(), name, func() error {
				called = true

				return nil
			})
			require.ErrorIs(t, err, state.ErrInvalidClusterName)
			assert.False(t, called)
		})
	}
}

// TestEKSLifecycleLockPreservesActionFailureAndReleases preserves errors and releases ownership.
func TestEKSLifecycleLockPreservesActionFailureAndReleases(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := state.WithEKSLifecycleLock(t.Context(), "demo", func() error {
		return errLifecycleAction
	})
	require.ErrorIs(t, err, errLifecycleAction)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	require.NoError(t, state.WithEKSLifecycleLock(ctx, "demo", func() error { return nil }))
}

// TestEKSLifecycleLockSurvivesStateDeletionAndProcessExit checks inode stability and OS process-exit cleanup.
func TestEKSLifecycleLockSurvivesStateDeletionAndProcessExit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, readyFile := startEKSLifecycleLockProcess(t)

	waited := false

	t.Cleanup(func() {
		if !waited {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	require.Eventually(t, func() bool {
		info, statErr := os.Stat(readyFile.Name())

		return statErr == nil && info.Size() > 0
	}, 5*time.Second, 10*time.Millisecond)

	// Removing and recreating cluster state must not replace the inode holding the lock.
	require.NoError(t, state.DeleteClusterState("demo"))

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	called := false
	err := state.WithEKSLifecycleLock(ctx, "demo", func() error {
		called = true

		return nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, called)
	// An unrelated cluster can proceed while the first process owns demo.
	require.NoError(
		t,
		state.WithEKSLifecycleLock(t.Context(), "other", func() error { return nil }),
	)

	require.NoError(t, child.Process.Kill())
	err = child.Wait()
	waited = true

	require.Error(t, err)

	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	require.NoError(t, state.WithEKSLifecycleLock(ctx, "demo", func() error { return nil }))
}

// startEKSLifecycleLockProcess starts this test binary with an inherited readiness descriptor.
func startEKSLifecycleLockProcess(t *testing.T) (*exec.Cmd, *os.File) {
	t.Helper()

	readyFile, err := os.CreateTemp(t.TempDir(), "ready")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, readyFile.Close()) })

	executable, err := os.Executable()
	require.NoError(t, err)
	//nolint:gosec // run this test binary with a private readiness marker.
	child := exec.CommandContext(
		t.Context(),
		executable,
		"-test.run=^TestEKSLifecycleLockProcess$",
		"-test.timeout=30s",
	)
	// Pass an already-open marker to the child instead of an environment-controlled write path.
	child.Stdout = readyFile

	child.Env = append(
		os.Environ(),
		"KSAIL_LOCK_PROCESS=1",
		"KSAIL_LOCK_HOME="+os.Getenv("HOME"),
	)
	require.NoError(t, child.Start())

	return child, readyFile
}

// TestEKSLifecycleLockProcess holds a lock in the subprocess until its parent terminates it.
func TestEKSLifecycleLockProcess(t *testing.T) {
	if os.Getenv("KSAIL_LOCK_PROCESS") != "1" {
		return
	}

	t.Setenv("HOME", os.Getenv("KSAIL_LOCK_HOME"))
	err := state.WithEKSLifecycleLock(t.Context(), "demo", func() error {
		_, writeErr := fmt.Fprintln(os.Stdout, "ready")
		if writeErr != nil {
			return fmt.Errorf("signal readiness: %w", writeErr)
		}

		<-t.Context().Done()

		return t.Context().Err()
	})
	require.NoError(t, err)
}
