package hetznertalos

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errInjectedWrite = errors.New("injected second write failure")

func TestWriteSourcesRollsBackEarlierFileWhenLaterWriteFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	firstPath := filepath.Join(dir, "defaults.go")
	secondPath := filepath.Join(dir, "options.go")

	require.NoError(t, os.WriteFile(firstPath, []byte("old defaults"), 0o600))
	require.NoError(t, os.WriteFile(secondPath, []byte("old options"), 0o600))

	files := []sourceFile{
		{path: firstPath, content: []byte("new defaults"), mode: 0o600},
		{path: secondPath, content: []byte("new options"), mode: 0o600},
	}
	calls := 0
	writeFile := func(path string, content []byte, mode os.FileMode) error {
		calls++
		if calls == 2 {
			return errInjectedWrite
		}

		return fsutil.AtomicWriteFile(path, content, mode)
	}

	err := writeSourcesWith(files, writeFile)
	require.ErrorIs(t, err, errInjectedWrite)
	assert.Equal(t, 3, calls, "the successful first write must be rolled back")

	firstContent, readErr := os.ReadFile(firstPath) //nolint:gosec // Test fixture path.
	require.NoError(t, readErr)
	secondContent, readErr := os.ReadFile(secondPath) //nolint:gosec // Test fixture path.
	require.NoError(t, readErr)
	assert.Equal(t, "old defaults", string(firstContent))
	assert.Equal(t, "old options", string(secondContent))
}
