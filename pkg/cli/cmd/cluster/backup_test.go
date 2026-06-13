package cluster_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The backup/restore engine (archive format, sanitization, tar create/extract,
// restore labeling) lives in pkg/svc/backup and is tested there. These tests
// cover only the cobra-wrapper helpers that remain in the cmd package.

func TestPrepareOutputPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "simple file in current dir",
			path:    filepath.Join(t.TempDir(), "backup.tar.gz"),
			wantErr: false,
		},
		{
			name:    "nested directory creation",
			path:    filepath.Join(t.TempDir(), "sub", "dir", "backup.tar.gz"),
			wantErr: false,
		},
		{
			name:    "current directory",
			path:    "backup.tar.gz",
			wantErr: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := cluster.ExportPrepareOutputPath(testCase.path)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, result)
			}
		})
	}
}

func TestPrintBackupSummary(t *testing.T) {
	t.Parallel()

	t.Run("existing file", func(t *testing.T) {
		t.Parallel()

		tmpFile := filepath.Join(t.TempDir(), "test.tar.gz")
		require.NoError(t, os.WriteFile(tmpFile, []byte("test data"), 0o600))

		var buf bytes.Buffer
		cluster.ExportPrintBackupSummary(&buf, tmpFile)

		output := buf.String()
		assert.Contains(t, output, "Backup completed successfully")
		assert.Contains(t, output, "Archive size:")
	})

	t.Run("nonexistent file", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		cluster.ExportPrintBackupSummary(&buf, "/nonexistent/path")

		output := buf.String()
		assert.Contains(t, output, "Backup completed successfully")
		assert.NotContains(t, output, "Archive size:")
	})
}
