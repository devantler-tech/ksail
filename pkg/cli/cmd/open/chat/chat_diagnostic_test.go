package chat_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/open/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const osWindows = "windows"

// probeTimeout is the deadline tests give the CLI probes when they spawn a real
// process. The probes return as soon as the process exits, so a generous ceiling
// costs nothing on an idle machine, while the production deadlines (a few seconds)
// can expire before a trivial script starts when the full test suite loads the host.
const probeTimeout = time.Minute

// hangTimeout is a short deadline for proving a probe still stops a process that
// never exits.
const hangTimeout = 200 * time.Millisecond

// errFakeCLIExit is a static sentinel used in TestStartupErrFmt to simulate
// the error produced by the Copilot SDK when its CLI subprocess exits early.
var errFakeCLIExit = errors.New("CLI process exited: exit status 1")

func writeScript(t *testing.T, dir, content string) string {
	t.Helper()

	path := filepath.Join(dir, "copilot")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(
		t,
		os.Chmod(path, 0o500), //nolint:gosec // scripts need execute bit; file lives in a temp dir
	)

	return path
}

func TestDiagnoseCLIStartupFailure(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == osWindows {
		t.Skip("test relies on shell scripts")
	}

	diagnose := chat.GetDiagnoseCLIStartupFailureWithin()

	t.Run("captures stderr from failing process", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\necho 'Error: missing config' >&2\nexit 1\n")

		result := diagnose(context.Background(), probeTimeout, script, "", os.Environ())
		assert.Equal(t, "Error: missing config", result)
	})

	t.Run("returns empty string when no stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\nexit 1\n")

		result := diagnose(context.Background(), probeTimeout, script, "", os.Environ())
		assert.Empty(t, result)
	})

	t.Run("returns empty string on cancelled context", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\nsleep 60\n")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result := diagnose(ctx, probeTimeout, script, "", os.Environ())
		assert.Empty(t, result)
	})

	t.Run("captures multiline stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir,
			"#!/bin/sh\necho 'line 1' >&2\necho 'line 2' >&2\nexit 1\n")

		result := diagnose(context.Background(), probeTimeout, script, "", os.Environ())
		assert.Equal(t, "line 1\nline 2", result)
	})

	t.Run("stops a process that never exits once the deadline passes", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		// exec replaces the shell, so the deadline kills the process holding the
		// stderr pipe and the probe returns instead of waiting for sleep to end.
		// It sleeps past probeTimeout, so ignoring the deadline fails the check below.
		script := writeScript(t, dir, "#!/bin/sh\nexec sleep 120\n")
		start := time.Now()

		result := diagnose(context.Background(), hangTimeout, script, "", os.Environ())
		assert.Empty(t, result)
		assert.Less(t, time.Since(start), probeTimeout, "the deadline must stop a hanging CLI")
	})

	t.Run("production deadline is unchanged", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 3*time.Second, chat.DiagnoseTimeout)
	})
}

func TestBuildDiagnosticBlock(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == osWindows {
		t.Skip("test relies on shell scripts")
	}

	build := chat.GetBuildDiagnosticBlock()

	t.Run("returns empty string for empty cliPath", func(t *testing.T) {
		t.Parallel()

		result := build(context.Background(), "", "", os.Environ())
		assert.Empty(t, result)
	})

	t.Run("returns empty string when no stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\nexit 1\n")

		result := build(context.Background(), script, "", os.Environ())
		assert.Empty(t, result)
	})
}

func TestFormatDiagnosticOutput(t *testing.T) {
	t.Parallel()

	t.Run("formats single-line output as indented block", func(t *testing.T) {
		t.Parallel()

		result := chat.FormatDiagnosticOutput("not logged in")
		assert.Equal(t, "CLI diagnostic output:\n  not logged in\n\n", result)
	})

	t.Run("indents each line of multiline output", func(t *testing.T) {
		t.Parallel()

		result := chat.FormatDiagnosticOutput("line 1\nline 2")
		assert.Equal(t, "CLI diagnostic output:\n  line 1\n  line 2\n\n", result)
	})
}

func TestStartupErrFmt(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == osWindows {
		t.Skip("test relies on shell scripts")
	}

	t.Run("includes diagnostic block in error when CLI writes stderr", func(t *testing.T) {
		t.Parallel()

		block := chat.FormatDiagnosticOutput("not logged in")
		err := fmt.Errorf(chat.StartupErrFmt, errFakeCLIExit, block)

		require.ErrorIs(t, err, errFakeCLIExit)
		assert.Contains(t, err.Error(), "CLI diagnostic output:")
		assert.Contains(t, err.Error(), "not logged in")
		assert.Contains(t, err.Error(), "To fix:")
	})

	t.Run("omits diagnostic section when CLI writes no stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\nexit 1\n")

		build := chat.GetBuildDiagnosticBlock()
		block := build(context.Background(), script, "", os.Environ())

		err := fmt.Errorf(chat.StartupErrFmt, errFakeCLIExit, block)

		require.ErrorIs(t, err, errFakeCLIExit)
		assert.NotContains(t, err.Error(), "CLI diagnostic output:")
		assert.Contains(t, err.Error(), "To fix:")
	})

	t.Run("percent signs in CLI stderr are not treated as format verbs", func(t *testing.T) {
		t.Parallel()

		block := chat.FormatDiagnosticOutput("100% complete but failed")
		err := fmt.Errorf(chat.StartupErrFmt, errFakeCLIExit, block)

		assert.Contains(t, err.Error(), "100% complete but failed")
	})
}
