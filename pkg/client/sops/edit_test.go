package sops_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	sopsclient "github.com/devantler-tech/ksail/v7/pkg/client/sops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven hashing/editor cases are easier to keep together.
func TestHashFile(t *testing.T) {
	t.Parallel()

	t.Run("hashes file content", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		filePath := filepath.Join(dir, "test.txt")
		err := os.WriteFile(filePath, []byte("hello world"), 0o600)
		require.NoError(t, err)

		hash, err := sopsclient.HashFile(filePath)

		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.Len(t, hash, 32, "SHA256 produces 32-byte hash")
	})

	t.Run("same content produces same hash", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		file1 := filepath.Join(dir, "file1.txt")
		file2 := filepath.Join(dir, "file2.txt")
		content := []byte("identical content")

		require.NoError(t, os.WriteFile(file1, content, 0o600))
		require.NoError(t, os.WriteFile(file2, content, 0o600))

		hash1, err := sopsclient.HashFile(file1)
		require.NoError(t, err)

		hash2, err := sopsclient.HashFile(file2)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("different content produces different hash", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		file1 := filepath.Join(dir, "file1.txt")
		file2 := filepath.Join(dir, "file2.txt")

		require.NoError(t, os.WriteFile(file1, []byte("content A"), 0o600))
		require.NoError(t, os.WriteFile(file2, []byte("content B"), 0o600))

		hash1, err := sopsclient.HashFile(file1)
		require.NoError(t, err)

		hash2, err := sopsclient.HashFile(file2)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		t.Parallel()

		_, err := sopsclient.HashFile("/nonexistent/file.txt")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open file")
	})

	t.Run("empty file produces valid hash", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		filePath := filepath.Join(dir, "empty.txt")
		require.NoError(t, os.WriteFile(filePath, []byte{}, 0o600))

		hash, err := sopsclient.HashFile(filePath)

		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.Len(t, hash, 32)
	})
}

//nolint:varnamelen // Short names keep this table-driven test readable.
func TestParseEditorCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		editor  string
		envVar  string
		want    []string
		wantErr bool
	}{
		{
			name:   "simple editor name",
			editor: "vim",
			envVar: "EDITOR",
			want:   []string{"vim"},
		},
		{
			name:   "editor with arguments",
			editor: "code --wait",
			envVar: "SOPS_EDITOR",
			want:   []string{"code", "--wait"},
		},
		{
			name:   "editor with multiple arguments",
			editor: "subl --new-window --wait",
			envVar: "EDITOR",
			want:   []string{"subl", "--new-window", "--wait"},
		},
		{
			name:    "empty editor string returns error",
			editor:  "",
			envVar:  "EDITOR",
			wantErr: true,
		},
		{
			name:    "whitespace-only editor string returns error",
			editor:  "   ",
			envVar:  "SOPS_EDITOR",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := sopsclient.ParseEditorCommand(tc.editor, tc.envVar)

			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, sopsclient.ErrInvalidEditor)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestLookupAnyEditor(t *testing.T) {
	t.Parallel()

	t.Run("no editors available returns error", func(t *testing.T) {
		t.Parallel()

		_, err := sopsclient.LookupAnyEditor("nonexistent-editor-1", "nonexistent-editor-2")

		require.Error(t, err)
		require.ErrorIs(t, err, sopsclient.ErrNoEditorAvailable)
		assert.Contains(t, err.Error(), "nonexistent-editor-1")
		assert.Contains(t, err.Error(), "nonexistent-editor-2")
	})

	t.Run("finds available editor", func(t *testing.T) {
		t.Parallel()

		editor := "sh"
		if runtime.GOOS == "windows" {
			editor = "cmd"
		}

		path, err := sopsclient.LookupAnyEditor("nonexistent-editor", editor)

		require.NoError(t, err)
		assert.NotEmpty(t, path)
	})

	t.Run("returns first available", func(t *testing.T) {
		t.Parallel()

		primaryEditor := "sh"
		secondaryEditor := "bash"

		if runtime.GOOS == "windows" {
			primaryEditor = "cmd"
			secondaryEditor = "powershell"
		}

		path, err := sopsclient.LookupAnyEditor(primaryEditor, secondaryEditor)

		require.NoError(t, err)
		assert.NotEmpty(t, path)
		assert.Contains(t, strings.ToLower(filepath.Base(path)), primaryEditor)
	})
}
