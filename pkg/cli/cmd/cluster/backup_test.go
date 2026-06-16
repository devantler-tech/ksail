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

func TestResolveArchivePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		flagValue   string
		want        string
		wantErr     bool
		wantErrPath bool
	}{
		{
			name:      "positional argument wins",
			args:      []string{"./positional.tar.gz"},
			flagValue: "./flag.tar.gz",
			want:      "./positional.tar.gz",
		},
		{
			name:      "deprecated flag used when no positional",
			args:      nil,
			flagValue: "./flag.tar.gz",
			want:      "./flag.tar.gz",
		},
		{
			name:        "neither provided errors",
			args:        nil,
			flagValue:   "",
			wantErr:     true,
			wantErrPath: true,
		},
		{
			name:      "empty positional falls back to flag",
			args:      []string{""},
			flagValue: "./flag.tar.gz",
			want:      "./flag.tar.gz",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := cluster.ExportResolveArchivePath(
				testCase.args, testCase.flagValue, "--output",
			)
			if testCase.wantErr {
				require.Error(t, err)

				if testCase.wantErrPath {
					require.ErrorIs(t, err, cluster.ErrArchivePathRequired)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestNewBackupCmd_DeprecatedAliases verifies the --output flag is deprecated and
// the -n shorthand on --namespaces is deprecated (reserved for --name).
func TestNewBackupCmd_DeprecatedAliases(t *testing.T) {
	t.Parallel()

	backupCmd := cluster.NewBackupCmd()
	require.NotNil(t, backupCmd)

	outputFlag := backupCmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "deprecated --output flag should still be registered")
	assert.NotEmpty(t, outputFlag.Deprecated, "--output should be marked deprecated")

	nsFlag := backupCmd.Flags().Lookup("namespaces")
	require.NotNil(t, nsFlag, "--namespaces should be registered")
	assert.Equal(t, "n", nsFlag.Shorthand, "-n shorthand should still be attached")
	assert.NotEmpty(t, nsFlag.ShorthandDeprecated,
		"-n shorthand should be marked deprecated (reserved for --name)")
	assert.Empty(t, nsFlag.Deprecated,
		"the long --namespaces flag itself must NOT be deprecated")
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
