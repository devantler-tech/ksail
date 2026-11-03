package caskcompletion_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/caskcompletion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestNativeCancellationKillsCompletionIgnoringHangupAndTerminate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sentinel := exec.CommandContext(t.Context(), "/bin/sleep", "15")
	require.NoError(t, sentinel.Start())
	t.Cleanup(func() { _ = sentinel.Process.Kill(); _ = sentinel.Wait() })
	brew, pidPath := stubbornCompletion(t, root, false)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	finished := make(chan error, 1)

	go func() {
		_, err := caskcompletion.ProbeForTest(ctx, brew, "ksail cl\t", root)
		finished <- err
	}()

	pid := recordedCompletionChild(t, pidPath)
	processGroup, err := unix.Getpgid(pid)
	require.NoError(t, err)
	assert.NotEqual(t, unix.Getpgrp(), processGroup, "the PTY owns a separate kernel process group")

	session, err := unix.Getsid(pid)
	require.NoError(t, err)
	parentSession, err := unix.Getsid(0)
	require.NoError(t, err)
	assert.NotEqual(t, parentSession, session, "script creates a separate PTY session")
	t.Logf("completion has PGID separate from PTY session leader: %t", processGroup != session)

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal("test deadline before cancellation")
	}

	cancel()

	select {
	case err := <-finished:
		require.ErrorIs(t, err, caskcompletion.ErrNoObservation)
	case <-time.After(4 * time.Second):
		t.Fatal("native cancellation did not return within its cleanup bound")
	}

	assertCompletionDead(t, pid)
	assert.NoError(
		t,
		unix.Kill(sentinel.Process.Pid, 0),
		"cleanup must preserve the caller's other child",
	)
}

func TestNativeSuccessfulTabRetiresDisownedCompletionWithoutChangingResult(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	brew, pidPath := stubbornCompletion(t, root, true)

	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()

	type outcome struct {
		result caskcompletion.Result
		err    error
	}

	finished := make(chan outcome, 1)

	go func() {
		result, err := caskcompletion.ProbeForTest(ctx, brew, "ksail cl\t", root)
		finished <- outcome{result: result, err: err}
	}()

	pid := recordedCompletionChild(t, pidPath)

	select {
	case observed := <-finished:
		require.NoError(t, observed.err)
		assert.Equal(t, "ksail cluster ", observed.result.ObservedBuffer)
		assert.True(t, observed.result.TabObserved)
	case <-time.After(4 * time.Second):
		t.Fatal("normal terminal exit did not return within its cleanup bound")
	}

	assertCompletionDead(t, pid)
}

func stubbornCompletion(t *testing.T, root string, background bool) (string, string) {
	t.Helper()
	brew := fixtureBrew(t, root, true)
	pidPath := filepath.Join(root, "completion.pid")

	command := fmt.Sprintf("/bin/zsh -c %s child %s", shellQuote(
		`trap '' HUP TERM; print -r -- $$ > "$1"; while true; do :; done`), shellQuote(pidPath))
	if background {
		command += " &!\nwhile [[ ! -e " + shellQuote(
			pidPath+".release",
		) + " ]]; do :; done\ncompadd cluster"
	}

	completion := "#compdef ksail\n_ksail() {\n" + command + "\n}\n"
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, "functions", "_ksail"), []byte(completion), 0o600),
	)

	return brew, pidPath
}

func recordedCompletionChild(t *testing.T, pidPath string) int {
	t.Helper()

	var pid int

	require.Eventually(t, func() bool {
		//nolint:gosec // This is the t.TempDir-owned fixture's PID file.
		data, err := os.ReadFile(
			pidPath,
		)
		if err != nil {
			return false
		}

		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))

		return err == nil && pid > 1
	}, 3*time.Second, 10*time.Millisecond, "completion must actually start")

	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	require.NoError(t, err)
	t.Cleanup(func() {
		current, queryErr := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if queryErr == nil && current.Proc.P_starttime == process.Proc.P_starttime {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	require.NoError(t, unix.Kill(pid, 0), "the recorded completion must be alive")
	require.NoError(t, os.WriteFile(pidPath+".release", nil, 0o600))

	return pid
}

func assertCompletionDead(t *testing.T, pid int) {
	t.Helper()
	assert.Eventually(
		t,
		func() bool { return errors.Is(unix.Kill(pid, 0), unix.ESRCH) },
		300*time.Millisecond,
		10*time.Millisecond,
		"owned completion must be dead when the probe returns",
	)
}

// Transient query errors must not abandon a live, TERM-resistant owned PTY session.
func TestNativeTerminationRetriesTransientProcessQueries(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, query string
		phase       int
	}{
		{"capture listing", "list", 1},
		{"TERM listing", "list", 2},
		{"KILL listing", "list", 3},
		{"liveness listing", "list", 4},
		{"capture session", "session", 1},
		{"TERM session", "session", 2},
		{"KILL session", "session", 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			guard, pid, sentinel := ownedTerminal(t)
			err := guard.StopWithQueryFaultForTest(testCase.query, testCase.phase, false, unix.EIO)

			assertCompletionDead(t, pid)
			require.NoError(t, err)
			assert.NoError(t, unix.Kill(sentinel, 0), "caller-session sibling must survive")
		})
	}
}

// Persistent uncertainty must remain an error within the existing cleanup budget.
func TestNativeTerminationRefusesPersistentProcessQueryFailure(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, query string
		phase       int
	}{
		{"capture listing", "list", 1},
		{"TERM session", "session", 2},
		{"liveness listing", "list", 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			guard, _, sentinel := ownedTerminal(t)
			started := time.Now()
			err := guard.StopWithQueryFaultForTest(testCase.query, testCase.phase, true, unix.EIO)
			require.ErrorIs(t, err, unix.EIO)
			assert.GreaterOrEqual(
				t,
				time.Since(started),
				time.Second,
				"retry within the existing shared budget",
			)
			assert.Less(t, time.Since(started), 1500*time.Millisecond)
			assert.NoError(
				t,
				unix.Kill(sentinel, 0),
				"unknown ownership cannot authorize unrelated kills",
			)
		})
	}
}

// ownedTerminal starts script's real Darwin PTY and a disowned child with its own job group.
func ownedTerminal(t *testing.T) (*caskcompletion.TerminalGuardForTest, int, int) {
	t.Helper()
	root := t.TempDir()
	pidPath := filepath.Join(root, "terminal-child.pid")
	sentinel := exec.CommandContext(t.Context(), "/bin/sleep", "15")
	require.NoError(t, sentinel.Start())
	t.Cleanup(func() { _ = sentinel.Process.Kill(); _ = sentinel.Wait() })

	child := `/bin/zsh -c 'trap "" HUP TERM; print -r -- $$ > "$1"; while true; do :; done' child ` + shellQuote(
		pidPath,
	)
	command := exec.CommandContext( //nolint:gosec // Fixed zsh code with a quoted, t.TempDir-owned PID path.
		t.Context(),
		"/usr/bin/script",
		"-q",
		"/dev/null",
		"/bin/zsh",
		"-c",
		child+" &!\nwhile true; do /bin/sleep 1; done",
	)
	guard := caskcompletion.NewTerminalGuardForTest(command)
	require.NoError(t, command.Start())
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	pid := recordedCompletionChild(t, pidPath)
	require.NoError(t, guard.CaptureForTest())

	session, err := unix.Getsid(pid)
	require.NoError(t, err)
	callerSession, err := unix.Getsid(0)
	require.NoError(t, err)
	require.NotEqual(
		t,
		callerSession,
		session,
		"test must exercise script's actual owned PTY session",
	)

	return guard, pid, sentinel.Process.Pid
}
