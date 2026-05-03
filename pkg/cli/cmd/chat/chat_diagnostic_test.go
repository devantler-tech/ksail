package chat_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeScript(t *testing.T, dir, content string) string {
	t.Helper()

	path := filepath.Join(dir, "copilot")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.Chmod(path, 0o500))

	return path
}

func TestDiagnoseCLIStartupFailure(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("test relies on shell scripts")
	}

	diagnose := chat.GetDiagnoseCLIStartupFailure()

	t.Run("captures stderr from failing process", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\necho 'Error: missing config' >&2\nexit 1\n")

		result := diagnose(context.Background(), script, os.Environ())
		assert.Equal(t, "Error: missing config", result)
	})

	t.Run("returns empty string when no stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\nexit 1\n")

		result := diagnose(context.Background(), script, os.Environ())
		assert.Empty(t, result)
	})

	t.Run("returns empty string on cancelled context", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\nsleep 60\n")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result := diagnose(ctx, script, os.Environ())
		assert.Empty(t, result)
	})

	t.Run("captures multiline stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir,
			"#!/bin/sh\necho 'line 1' >&2\necho 'line 2' >&2\nexit 1\n")

		result := diagnose(context.Background(), script, os.Environ())
		assert.Equal(t, "line 1\nline 2", result)
	})
}

func TestBuildDiagnosticBlock(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("test relies on shell scripts")
	}

	build := chat.GetBuildDiagnosticBlock()

	t.Run("returns empty string for empty cliPath", func(t *testing.T) {
		t.Parallel()

		result := build(context.Background(), "", os.Environ())
		assert.Empty(t, result)
	})

	t.Run("returns empty string when no stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\nexit 1\n")

		result := build(context.Background(), script, os.Environ())
		assert.Empty(t, result)
	})

	t.Run("formats stderr as indented block", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\necho 'not logged in' >&2\nexit 1\n")

		result := build(context.Background(), script, os.Environ())
		assert.Equal(t, "CLI diagnostic output:\n  not logged in\n\n", result)
	})

	t.Run("indents multiline stderr correctly", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\necho 'line 1' >&2\necho 'line 2' >&2\nexit 1\n")

		result := build(context.Background(), script, os.Environ())
		assert.Equal(t, "CLI diagnostic output:\n  line 1\n  line 2\n\n", result)
	})
}

func TestStartupErrFmt(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("test relies on shell scripts")
	}

	t.Run("includes diagnostic block in error when CLI writes stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\necho 'not logged in' >&2\nexit 1\n")

		build := chat.GetBuildDiagnosticBlock()
		block := build(context.Background(), script, os.Environ())

		cause := errors.New("CLI process exited: exit status 1")
		err := fmt.Errorf(chat.StartupErrFmt, cause, block)

		assert.ErrorIs(t, err, cause)
		assert.True(t, strings.Contains(err.Error(), "CLI diagnostic output:"))
		assert.True(t, strings.Contains(err.Error(), "not logged in"))
		assert.True(t, strings.Contains(err.Error(), "To fix:"))
	})

	t.Run("omits diagnostic section when CLI writes no stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\nexit 1\n")

		build := chat.GetBuildDiagnosticBlock()
		block := build(context.Background(), script, os.Environ())

		cause := errors.New("CLI process exited: exit status 1")
		err := fmt.Errorf(chat.StartupErrFmt, cause, block)

		assert.ErrorIs(t, err, cause)
		assert.False(t, strings.Contains(err.Error(), "CLI diagnostic output:"))
		assert.True(t, strings.Contains(err.Error(), "To fix:"))
	})

	t.Run("percent signs in CLI stderr are not treated as format verbs", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "#!/bin/sh\nprintf '100%% complete but failed' >&2\nexit 1\n")

		build := chat.GetBuildDiagnosticBlock()
		block := build(context.Background(), script, os.Environ())

		cause := errors.New("CLI process exited: exit status 1")
		err := fmt.Errorf(chat.StartupErrFmt, cause, block)

		assert.True(t, strings.Contains(err.Error(), "100% complete but failed"))
	})
}
