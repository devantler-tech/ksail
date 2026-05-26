package chat_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeExecutable writes a runnable script that is also writable by its owner
// (0o700), so tests can open it O_WRONLY to provoke a deterministic ETXTBSY.
func writeExecutable(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "prog")

	err := os.WriteFile(path, []byte(content), 0o700) //nolint:gosec // test fixture
	require.NoError(t, err)

	return path
}

// openWriteLockedExecutable writes a script and holds it open for writing for
// the test's lifetime. On Linux, exec'ing a file open for writing fails with
// ETXTBSY deterministically, which lets us drive the retry branch.
func openWriteLockedExecutable(t *testing.T) string {
	t.Helper()

	path := writeExecutable(t, "#!/bin/sh\nexit 0\n")

	file, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // test fixture
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	return path
}

// writeSettledExecutable writes a runnable script and then execs it until the
// launch no longer races a concurrent fork that still holds a write fd to the
// freshly written file (which Linux reports as ETXTBSY). The only write fd to
// the file comes from the single os.WriteFile in writeExecutable, so once any
// launch returns a non-ETXTBSY result no process holds a write fd and every
// subsequent exec is deterministic.
//
// Use this when a test asserts an exact exec attempt count. Heavy parallel CI
// load can otherwise inject a transient ETXTBSY into the file's first exec,
// which runCopilotCmdWithRetry legitimately retries — inflating the count and
// flaking the assertion. On macOS ETXTBSY is not enforced here, so the loop
// returns after a single exec.
func writeSettledExecutable(t *testing.T, content string) string {
	t.Helper()

	path := writeExecutable(t, content)

	deadline := time.Now().Add(2 * time.Second)

	for {
		err := exec.CommandContext(context.Background(), path).Run() //nolint:gosec // test fixture
		if !errors.Is(err, syscall.ETXTBSY) {
			return path
		}

		if time.Now().After(deadline) {
			t.Fatal("freshly written script never settled: exec kept returning ETXTBSY")
		}

		time.Sleep(chat.CopilotExecRetryBackoff)
	}
}

func TestVerifyCopilotCLI(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == osWindows {
		t.Skip("test relies on shell scripts")
	}

	verify := chat.GetVerifyCopilotCLI()

	t.Run("returns nil when the CLI exits zero", func(t *testing.T) {
		t.Parallel()

		script := writeExecutable(t, "#!/bin/sh\necho 'copilot 1.2.3'\nexit 0\n")

		require.NoError(t, verify(context.Background(), script, os.Environ()))
	})

	t.Run("wraps a pre-flight failure with the CLI output", func(t *testing.T) {
		t.Parallel()

		script := writeExecutable(t, "#!/bin/sh\necho 'broken install' >&2\nexit 1\n")

		err := verify(context.Background(), script, os.Environ())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pre-flight check")
		assert.Contains(t, err.Error(), "broken install")
	})
}

func TestRunCopilotAuthLogin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == osWindows {
		t.Skip("test relies on shell scripts")
	}

	login := chat.GetRunCopilotAuthLogin()

	t.Run("returns nil when login succeeds", func(t *testing.T) {
		t.Parallel()

		script := writeExecutable(t, "#!/bin/sh\nexit 0\n")

		require.NoError(t, login(context.Background(), script))
	})

	t.Run("wraps a login failure", func(t *testing.T) {
		t.Parallel()

		script := writeExecutable(t, "#!/bin/sh\nexit 3\n")

		err := login(context.Background(), script)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "auth login failed")
	})
}

func TestRunCopilotCmdWithRetry(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == osWindows {
		t.Skip("test relies on shell scripts")
	}

	run := chat.GetRunCopilotCmdWithRetry()

	t.Run("returns nil and runs once on success", func(t *testing.T) {
		t.Parallel()

		script := writeSettledExecutable(t, "#!/bin/sh\nexit 0\n")
		attempts := 0

		err := run(context.Background(), func() *exec.Cmd {
			attempts++

			return exec.CommandContext(context.Background(), script)
		})

		require.NoError(t, err)
		assert.Equal(t, 1, attempts, "a successful launch must not retry")
	})

	t.Run("does not retry a non-ETXTBSY failure", func(t *testing.T) {
		t.Parallel()

		script := writeSettledExecutable(t, "#!/bin/sh\nexit 1\n")
		attempts := 0

		err := run(context.Background(), func() *exec.Cmd {
			attempts++

			return exec.CommandContext(context.Background(), script)
		})

		require.Error(t, err)
		require.NotErrorIs(t, err, syscall.ETXTBSY)
		assert.Equal(t, 1, attempts, "an ordinary exit error must not retry")
	})
}

// TestRunCopilotCmdWithRetryETXTBSYExhaustion drives the retry branch with a
// deterministic ETXTBSY (a binary held open for writing cannot be exec'd).
// Linux enforces this reliably; macOS does not, so it is gated to Linux, which
// is both where the guarded race occurs and where CI measures coverage.
func TestRunCopilotCmdWithRetryETXTBSYExhaustion(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("ETXTBSY on exec of a write-open file is only reliable on Linux")
	}

	run := chat.GetRunCopilotCmdWithRetry()
	path := openWriteLockedExecutable(t)
	attempts := 0
	start := time.Now()

	err := run(context.Background(), func() *exec.Cmd {
		attempts++

		return exec.CommandContext(context.Background(), path)
	})

	require.ErrorIs(t, err, syscall.ETXTBSY)
	assert.Equal(t, chat.CopilotExecMaxRetries+1, attempts,
		"should make one initial attempt plus the bounded retries")
	assert.GreaterOrEqual(t, time.Since(start),
		time.Duration(chat.CopilotExecMaxRetries)*chat.CopilotExecRetryBackoff,
		"should wait the cumulative backoff between attempts")
}

// TestRunCopilotCmdWithRetryCancelDuringBackoff verifies that cancelling the
// context during the backoff window stops further attempts immediately and
// surfaces the cancellation reason rather than the transient ETXTBSY. The
// command's own context stays live so the launch still fails with ETXTBSY; only
// the retry context is cancelled.
func TestRunCopilotCmdWithRetryCancelDuringBackoff(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("ETXTBSY on exec of a write-open file is only reliable on Linux")
	}

	run := chat.GetRunCopilotCmdWithRetry()
	path := openWriteLockedExecutable(t)
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	err := run(ctx, func() *exec.Cmd {
		attempts++
		// Cancel after the first failed launch so the backoff select observes
		// ctx.Done() instead of waiting out the timer.
		cancel()

		return exec.CommandContext(context.Background(), path)
	})

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, attempts, "cancellation during backoff must stop further attempts")
}
