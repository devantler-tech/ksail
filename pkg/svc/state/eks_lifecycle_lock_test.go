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

func TestEKSLifecycleLockSurvivesStateDeletionAndProcessExit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	readyPath := filepath.Join(t.TempDir(), "ready")
	executable, err := os.Executable()
	require.NoError(t, err)
	//nolint:gosec // run this test binary with a private readiness marker.
	child := exec.CommandContext(
		t.Context(),
		executable,
		"-test.run=^TestEKSLifecycleLockProcess$",
		"-test.timeout=30s",
	)

	child.Env = append(
		os.Environ(),
		"KSAIL_LOCK_READY="+readyPath,
		"KSAIL_LOCK_HOME="+os.Getenv("HOME"),
	)
	require.NoError(t, child.Start())

	waited := false

	t.Cleanup(func() {
		if !waited {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(readyPath)

		return statErr == nil
	}, 5*time.Second, 10*time.Millisecond)

	// Removing and recreating cluster state must not replace the inode holding the lock.
	require.NoError(t, state.DeleteClusterState("demo"))

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	called := false
	err = state.WithEKSLifecycleLock(ctx, "demo", func() error {
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

func TestEKSLifecycleLockProcess(t *testing.T) {
	readyPath := os.Getenv("KSAIL_LOCK_READY")
	if readyPath == "" {
		return
	}

	t.Setenv("HOME", os.Getenv("KSAIL_LOCK_HOME"))
	err := state.WithEKSLifecycleLock(t.Context(), "demo", func() error {
		writeErr := os.WriteFile(readyPath, nil, 0o600)
		if writeErr != nil {
			return fmt.Errorf("write readiness marker: %w", writeErr)
		}

		<-t.Context().Done()

		return t.Context().Err()
	})
	require.NoError(t, err)
}
