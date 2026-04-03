package cluster_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRestoreErrorConstants verifies that all sentinel error variables
// defined in restore.go are non-nil and have meaningful messages.
func TestRestoreErrorConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
		wantMsg  string
	}{
		{
			name:     "ErrInvalidResourcePolicy is defined",
			sentinel: clusterpkg.ErrInvalidResourcePolicy,
			wantMsg:  "invalid existing-resource-policy",
		},
		{
			name:     "ErrRestoreFailed is defined",
			sentinel: clusterpkg.ErrRestoreFailed,
			wantMsg:  "resource restore failed",
		},
		{
			name:     "ErrInvalidTarPath is defined",
			sentinel: clusterpkg.ErrInvalidTarPath,
			wantMsg:  "invalid tar entry path",
		},
		{
			name:     "ErrSymlinkInArchive is defined",
			sentinel: clusterpkg.ErrSymlinkInArchive,
			wantMsg:  "symbolic and hard links are not supported",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, testCase.sentinel)
			assert.Contains(t, testCase.sentinel.Error(), testCase.wantMsg)
		})
	}
}

// TestRestoreErrors_AreDistinct verifies that all restore error sentinels
// are distinct from one another so errors.Is behaves correctly.
func TestRestoreErrors_AreDistinct(t *testing.T) {
	t.Parallel()

	allErrors := []error{
		clusterpkg.ErrInvalidResourcePolicy,
		clusterpkg.ErrRestoreFailed,
		clusterpkg.ErrInvalidTarPath,
		clusterpkg.ErrSymlinkInArchive,
	}

	for index := range allErrors {
		for innerIndex := index + 1; innerIndex < len(allErrors); innerIndex++ {
			assert.NotErrorIs(
				t,
				allErrors[index], allErrors[innerIndex],
				"errors at index %d and %d should be distinct",
				index, innerIndex,
			)
		}
	}
}

// TestRestoreErrors_CanBeWrapped verifies that sentinel errors can be wrapped
// with fmt.Errorf and still be detected via errors.Is.
func TestRestoreErrors_CanBeWrapped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
	}{
		{
			name:     "ErrInvalidResourcePolicy can be wrapped",
			sentinel: clusterpkg.ErrInvalidResourcePolicy,
		},
		{
			name:     "ErrRestoreFailed can be wrapped",
			sentinel: clusterpkg.ErrRestoreFailed,
		},
		{
			name:     "ErrInvalidTarPath can be wrapped",
			sentinel: clusterpkg.ErrInvalidTarPath,
		},
		{
			name:     "ErrSymlinkInArchive can be wrapped",
			sentinel: clusterpkg.ErrSymlinkInArchive,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("context: %w", testCase.sentinel)
			assert.ErrorIs(t, wrapped, testCase.sentinel)
		})
	}
}

// TestNewRestoreCmd_FlagsExistWithCorrectDefaults verifies that NewRestoreCmd
// registers all expected flags with the correct default values.
func TestNewRestoreCmd_FlagsExistWithCorrectDefaults(t *testing.T) {
	t.Parallel()

	flagTests := []struct {
		flagName     string
		shorthand    string
		defaultValue string
	}{
		{
			flagName:     "input",
			shorthand:    "i",
			defaultValue: "",
		},
		{
			flagName:     "existing-resource-policy",
			shorthand:    "",
			defaultValue: "none",
		},
		{
			flagName:     "dry-run",
			shorthand:    "",
			defaultValue: "false",
		},
	}

	for _, flagTest := range flagTests {
		t.Run(flagTest.flagName, func(t *testing.T) {
			t.Parallel()

			restoreCmd := clusterpkg.NewRestoreCmd(nil)
			require.NotNil(t, restoreCmd)

			flag := restoreCmd.Flags().Lookup(flagTest.flagName)
			require.NotNil(t, flag, "flag %q should be registered", flagTest.flagName)
			assert.Equal(t, flagTest.defaultValue, flag.DefValue,
				"flag %q should have default value %q", flagTest.flagName, flagTest.defaultValue)

			if flagTest.shorthand != "" {
				assert.Equal(t, flagTest.shorthand, flag.Shorthand,
					"flag %q should have shorthand %q", flagTest.flagName, flagTest.shorthand)
			}
		})
	}
}

// TestNewRestoreCmd_InputFlagIsRequired verifies that --input is marked required.
func TestNewRestoreCmd_InputFlagIsRequired(t *testing.T) {
	t.Parallel()

	restoreCmd := clusterpkg.NewRestoreCmd(nil)
	require.NotNil(t, restoreCmd)

	restoreCmd.SetOut(io.Discard)
	restoreCmd.SetErr(io.Discard)
	restoreCmd.SetArgs([]string{"--existing-resource-policy", "none"})

	err := restoreCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "input")
}

// TestRestoreCmd_InvalidResourcePolicy verifies that an invalid
// existing-resource-policy value returns ErrInvalidResourcePolicy.
// The policy validation in runRestore happens before kubeconfig and
// file access, so we do not need a real cluster or archive for this test.
func TestRestoreCmd_InvalidResourcePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy string
	}{
		{name: "unknown policy value", policy: "unknown"},
		{name: "capitalised none", policy: "None"},
		{name: "capitalised update", policy: "Update"},
		{name: "unsupported policy value 'skip'", policy: "skip"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			restoreCmd := clusterpkg.NewRestoreCmd(nil)
			restoreCmd.SetOut(io.Discard)
			restoreCmd.SetErr(io.Discard)
			restoreCmd.SetArgs([]string{
				"--input", "dummy.tar.gz",
				"--existing-resource-policy", testCase.policy,
			})

			err := restoreCmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, clusterpkg.ErrInvalidResourcePolicy,
				"expected ErrInvalidResourcePolicy, got: %v", err,
			)
		})
	}
}

// TestRestoreCmd_ValidPoliciesPassValidation verifies that "none" and "update"
// are accepted as valid policy values. The command will fail later when trying
// to open the nonexistent --input archive, NOT at the policy check.
func TestRestoreCmd_ValidPoliciesPassValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy string
	}{
		{name: "none policy", policy: "none"},
		{name: "update policy", policy: "update"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			nonexistentArchive := filepath.Join(t.TempDir(), "nonexistent.tar.gz")

			restoreCmd := clusterpkg.NewRestoreCmd(nil)
			restoreCmd.SetOut(io.Discard)
			restoreCmd.SetErr(io.Discard)
			restoreCmd.SetArgs([]string{
				"--input", nonexistentArchive,
				"--existing-resource-policy", testCase.policy,
			})

			err := restoreCmd.Execute()

			require.Error(
				t, err,
				"expected a later error (archive not found), not ErrInvalidResourcePolicy",
			)
			assert.NotErrorIs(t, err, clusterpkg.ErrInvalidResourcePolicy,
				"valid policy %q should not return ErrInvalidResourcePolicy", testCase.policy,
			)
		})
	}
}

// TestRestoreCmd_Metadata verifies basic command metadata.
func TestRestoreCmd_Metadata(t *testing.T) {
	t.Parallel()

	restoreCmd := clusterpkg.NewRestoreCmd(nil)
	require.NotNil(t, restoreCmd)

	assert.Equal(t, "restore", restoreCmd.Use)
	assert.NotEmpty(t, restoreCmd.Short)
	assert.NotEmpty(t, restoreCmd.Long)
	assert.True(t, restoreCmd.SilenceUsage)
}

// TestDeriveBackupName_ExtensionStripping verifies the extension stripping
// logic for .tar.gz and .tgz archives.
func TestDeriveBackupName_ExtensionStripping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tar.gz with path",
			input:    "/backups/cluster-backup.tar.gz",
			expected: "cluster-backup",
		},
		{
			name:     "tgz with path",
			input:    "/backups/cluster-backup.tgz",
			expected: "cluster-backup",
		},
		{
			name:     "simple filename",
			input:    "my-backup.tar.gz",
			expected: "my-backup",
		},
		{
			name:     "no extension",
			input:    "my-backup",
			expected: "my-backup",
		},
		{
			name:     "other extension preserved",
			input:    "my-backup.zip",
			expected: "my-backup.zip",
		},
		{
			name:     "timestamped name",
			input:    "/mnt/ksail-backup-2026-03-21T10:00:00Z.tar.gz",
			expected: "ksail-backup-2026-03-21T10:00:00Z",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := clusterpkg.ExportDeriveBackupName(testCase.input)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// TestAllLinesContain_EdgeCases tests additional edge cases for the
// allLinesContain helper used in restore's "already exists" detection.

//nolint:dupl
func TestAllLinesContain_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   string
		substr   string
		expected bool
	}{
		{
			name:     "single matching non-empty line",
			output:   "already exists",
			substr:   "already exists",
			expected: true,
		},
		{
			name:     "line with surrounding whitespace",
			output:   "  already exists  \n",
			substr:   "already exists",
			expected: true,
		},
		{
			name:     "mixed matching and non-matching",
			output:   "already exists\nother error",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "completely empty output",
			output:   "",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "all empty lines",
			output:   "\n\n\n",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "multiple all-matching lines",
			output:   "error: resource already exists\nerror: configmap already exists\nerror: secret already exists",
			substr:   "already exists",
			expected: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := clusterpkg.ExportAllLinesContain(testCase.output, testCase.substr)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// TestPrintRestoreHeader verifies that printRestoreHeader writes the expected
// lines including the input path, policy, and (when dry-run) the dry-run note.
func TestPrintRestoreHeader( //nolint:funlen // Table-driven test with multiple comprehensive cases
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name      string
		inputPath string
		policy    string
		dryRun    bool
		wantLines []string
		noLines   []string
	}{
		{
			name:      "standard header without dry-run",
			inputPath: "/backups/cluster.tar.gz",
			policy:    "none",
			dryRun:    false,
			wantLines: []string{
				"Starting cluster restore",
				"/backups/cluster.tar.gz",
				"none",
				"Extracting backup archive",
			},
			noLines: []string{"dry-run"},
		},
		{
			name:      "header with dry-run enabled",
			inputPath: "/backups/cluster.tar.gz",
			policy:    "update",
			dryRun:    true,
			wantLines: []string{
				"Starting cluster restore",
				"/backups/cluster.tar.gz",
				"update",
				"dry-run",
			},
		},
		{
			name:      "header with update policy",
			inputPath: "relative/path/backup.tar.gz",
			policy:    "update",
			dryRun:    false,
			wantLines: []string{
				"update",
				"relative/path/backup.tar.gz",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			clusterpkg.ExportPrintRestoreHeader(
				&buf, testCase.inputPath, testCase.policy, testCase.dryRun,
			)
			output := buf.String()

			for _, want := range testCase.wantLines {
				assert.Contains(t, output, want,
					"output should contain %q", want)
			}

			for _, noWant := range testCase.noLines {
				assert.NotContains(t, output, noWant,
					"output should not contain %q", noWant)
			}
		})
	}
}

// TestPrintRestoreMetadata verifies that printRestoreMetadata correctly outputs
// all metadata fields, including optional Distribution and Provider.
func TestPrintRestoreMetadata( //nolint:funlen // Table-driven test with multiple comprehensive cases
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name      string
		metadata  *clusterpkg.BackupMetadata
		wantLines []string
		noLines   []string
	}{
		{
			name: "full metadata with distribution and provider",
			metadata: &clusterpkg.BackupMetadata{
				Version:       "v1",
				Timestamp:     time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
				ClusterName:   "my-cluster",
				Distribution:  "Vanilla",
				Provider:      "Docker",
				ResourceCount: 42,
			},
			wantLines: []string{
				"v1",
				"2026-03-15",
				"my-cluster",
				"Vanilla",
				"Docker",
				"42",
			},
		},
		{
			name: "metadata without optional distribution and provider",
			metadata: &clusterpkg.BackupMetadata{
				Version:       "v1",
				Timestamp:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				ClusterName:   "bare-cluster",
				ResourceCount: 5,
			},
			wantLines: []string{
				"v1",
				"bare-cluster",
				"5",
			},
			noLines: []string{"Distribution:", "Provider:"},
		},
		{
			name: "zero resource count is printed",
			metadata: &clusterpkg.BackupMetadata{
				Version:     "v1",
				ClusterName: "empty-cluster",
			},
			wantLines: []string{"Resources: 0"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			clusterpkg.ExportPrintRestoreMetadata(&buf, testCase.metadata)
			output := buf.String()

			for _, want := range testCase.wantLines {
				assert.Contains(t, output, want,
					"output should contain %q", want)
			}

			for _, noWant := range testCase.noLines {
				assert.NotContains(t, output, noWant,
					"output should not contain %q", noWant)
			}
		})
	}
}

// TestReadBackupMetadata verifies error paths and happy path of readBackupMetadata.
func TestReadBackupMetadata( //nolint:funlen // Covers multiple distinct error and success paths
	t *testing.T,
) {
	t.Parallel()

	t.Run("returns error when metadata file is missing", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		_, err := clusterpkg.ExportReadBackupMetadata(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup metadata")
	})

	t.Run("returns error when metadata is not valid JSON", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		metaPath := filepath.Join(tmpDir, "backup-metadata.json")
		err := os.WriteFile(metaPath, []byte("{not valid json"), 0o600)
		require.NoError(t, err)

		_, err = clusterpkg.ExportReadBackupMetadata(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse backup metadata")
	})

	t.Run("returns metadata when file is valid JSON", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		meta := &clusterpkg.BackupMetadata{
			Version:       "v1",
			ClusterName:   "test",
			ResourceCount: 7,
		}

		data, err := json.Marshal(meta)
		require.NoError(t, err)

		metaPath := filepath.Join(tmpDir, "backup-metadata.json")
		err = os.WriteFile(metaPath, data, 0o600)
		require.NoError(t, err)

		result, err := clusterpkg.ExportReadBackupMetadata(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "v1", result.Version)
		assert.Equal(t, "test", result.ClusterName)
		assert.Equal(t, 7, result.ResourceCount)
	})

	t.Run("empty JSON object is parsed without error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		metaPath := filepath.Join(tmpDir, "backup-metadata.json")
		err := os.WriteFile(metaPath, []byte("{}"), 0o600)
		require.NoError(t, err)

		result, err := clusterpkg.ExportReadBackupMetadata(tmpDir)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result.Version)
	})
}

// TestExtractBackupArchive_ErrorPaths validates error handling when the
// archive is missing, corrupt, or lacks the expected metadata file.
func TestExtractBackupArchive_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("nonexistent archive returns error", func(t *testing.T) {
		t.Parallel()

		_, _, err := clusterpkg.ExportExtractBackupArchive(
			filepath.Join(t.TempDir(), "does-not-exist.tar.gz"),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup archive")
	})

	t.Run("non-gzip file returns gzip error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		badFile := filepath.Join(tmpDir, "bad.tar.gz")
		err := os.WriteFile(badFile, []byte("this is not gzip content"), 0o600)
		require.NoError(t, err)

		_, _, err = clusterpkg.ExportExtractBackupArchive(badFile)
		require.Error(t, err)
	})

	t.Run("valid gzip but empty content returns tar error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "empty.tar.gz")

		archiveFile, err := os.Create(archivePath) //nolint:gosec // test-controlled temp path
		require.NoError(t, err)

		gz := gzip.NewWriter(archiveFile)
		// Write a single byte so gzip stream is valid but not a valid tar archive
		_, err = gz.Write([]byte{0x00})
		require.NoError(t, err)
		require.NoError(t, gz.Close())
		require.NoError(t, archiveFile.Close())

		_, _, err = clusterpkg.ExportExtractBackupArchive(archivePath)
		require.Error(t, err)
	})

	t.Run("valid tar.gz without metadata returns error", func(t *testing.T) {
		t.Parallel()

		archivePath := createArchiveWithoutMetadata(t)

		_, _, err := clusterpkg.ExportExtractBackupArchive(archivePath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup metadata")
	})
}

// createArchiveWithoutMetadata creates a valid .tar.gz file that contains
// a single YAML file but no backup-metadata.json entry.
func createArchiveWithoutMetadata(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "no-meta.tar.gz")

	f, err := os.Create(archivePath) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	gzipWriter := gzip.NewWriter(f)
	tarWriter := tar.NewWriter(gzipWriter)

	content := []byte("apiVersion: v1\nkind: Pod\n")
	hdr := &tar.Header{
		Name:     "resources/pods.yaml",
		Typeflag: tar.TypeReg,
		Size:     int64(len(content)),
		Mode:     0o600,
	}

	err = tarWriter.WriteHeader(hdr)
	require.NoError(t, err)

	_, err = tarWriter.Write(content)
	require.NoError(t, err)

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())

	return archivePath
}

// TestExtractBackupArchive_HappyPath validates that a well-formed .tar.gz
// archive is correctly extracted and its metadata is returned.
func TestExtractBackupArchive_HappyPath(t *testing.T) {
	t.Parallel()

	archivePath := createValidArchive(t)

	tmpDir, meta, err := clusterpkg.ExportExtractBackupArchive(archivePath)
	require.NoError(t, err)

	defer func() { _ = os.RemoveAll(tmpDir) }()

	require.NotNil(t, meta)
	assert.Equal(t, "v1", meta.Version)
	assert.Equal(t, "test-cluster", meta.ClusterName)
	assert.Equal(t, 3, meta.ResourceCount)

	// The resources directory must exist inside the extracted temp dir.
	resourcesDir := filepath.Join(tmpDir, "resources")
	_, err = os.Stat(resourcesDir)
	require.NoError(t, err, "resources directory should be extracted")

	// The YAML file must be present.
	podFile := filepath.Join(resourcesDir, "pods.yaml")
	_, err = os.Stat(podFile)
	require.NoError(t, err, "pods.yaml should be extracted")
}

// createValidArchive builds a complete, valid backup .tar.gz that passes
// extractBackupArchive (includes backup-metadata.json and a resource file).
func createValidArchive(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "valid.tar.gz")

	f, err := os.Create(archivePath) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	gzipWriter := gzip.NewWriter(f)
	tarWriter := tar.NewWriter(gzipWriter)

	// Add backup-metadata.json.
	meta := `{"version":"v1","clusterName":"test-cluster","resourceCount":3}`
	addTarEntry(t, tarWriter, "backup-metadata.json", []byte(meta))

	// Add a resources directory entry.
	err = tarWriter.WriteHeader(&tar.Header{
		Name:     "resources/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	})
	require.NoError(t, err)

	// Add a YAML resource file.
	addTarEntry(t, tarWriter, "resources/pods.yaml", []byte("apiVersion: v1\nkind: Pod\n"))

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())

	return archivePath
}

// addTarEntry writes a regular file entry into the tar writer.
func addTarEntry(t *testing.T, tw *tar.Writer, name string, content []byte) {
	t.Helper()

	err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Size:     int64(len(content)),
		Mode:     0o600,
	})
	require.NoError(t, err)

	_, err = tw.Write(content)
	require.NoError(t, err)
}

// TestBackupResourceTypes verifies that backupResourceTypes returns a non-empty
// ordered slice and that CRDs appear before namespaces (dependency ordering).
func TestBackupResourceTypes(t *testing.T) {
	t.Parallel()

	types := clusterpkg.ExportBackupResourceTypes()

	require.NotEmpty(t, types, "backupResourceTypes must return a non-empty list")

	// Validate that the list contains well-known resource types.
	typeSet := make(map[string]struct{}, len(types))
	for _, rt := range types {
		typeSet[rt] = struct{}{}
	}

	for _, expected := range []string{
		"customresourcedefinitions",
		"namespaces",
		"deployments",
	} {
		assert.Contains(t, typeSet, expected,
			"backupResourceTypes should include %q", expected)
	}

	// CRDs must come before namespaces in restore ordering.
	crdIdx, nsIdx := -1, -1

	for i, rt := range types {
		switch rt {
		case "customresourcedefinitions":
			crdIdx = i
		case "namespaces":
			nsIdx = i
		}
	}

	assert.Greater(t, nsIdx, crdIdx,
		"customresourcedefinitions must appear before namespaces in resource ordering")

	// No duplicates allowed.
	seen := make(map[string]bool, len(types))
	for _, rt := range types {
		assert.False(t, seen[rt], "duplicate resource type %q in backupResourceTypes", rt)
		seen[rt] = true
	}

	// Every entry must be a non-empty, lowercase string without whitespace.
	for _, rt := range types {
		assert.NotEmpty(t, rt)
		assert.Equal(t, strings.ToLower(rt), rt,
			"resource type %q should be lowercase", rt)
		assert.NotContains(t, rt, " ",
			"resource type %q should not contain spaces", rt)
	}
}
