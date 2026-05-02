package chat_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))

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
		script := writeScript(t, dir, "copilot", "#!/bin/sh\necho 'Error: missing config' >&2\nexit 1\n")

		result := diagnose(context.Background(), script, os.Environ())
		assert.Equal(t, "Error: missing config", result)
	})

	t.Run("returns empty string when no stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "copilot", "#!/bin/sh\nexit 1\n")

		result := diagnose(context.Background(), script, os.Environ())
		assert.Empty(t, result)
	})

	t.Run("returns empty string on cancelled context", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "copilot", "#!/bin/sh\nsleep 60\n")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result := diagnose(ctx, script, os.Environ())
		assert.Empty(t, result)
	})

	t.Run("captures multiline stderr", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		script := writeScript(t, dir, "copilot",
			"#!/bin/sh\necho 'line 1' >&2\necho 'line 2' >&2\nexit 1\n")

		result := diagnose(context.Background(), script, os.Environ())
		assert.Equal(t, "line 1\nline 2", result)
	})
}
