package backup_test

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/backup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type validateTarEntryTest struct {
	name    string
	header  *tar.Header
	wantErr bool
	errType error
}

func pathTraversalTestCases() []validateTarEntryTest {
	return []validateTarEntryTest{
		{
			name:   "valid regular file",
			header: &tar.Header{Name: tPodsYAMLPath, Typeflag: tar.TypeReg},
		},
		{
			name:   "valid directory",
			header: &tar.Header{Name: tResourcesDir, Typeflag: tar.TypeDir},
		},
		{
			name:    "absolute path",
			header:  &tar.Header{Name: tEtcPasswd, Typeflag: tar.TypeReg},
			wantErr: true, errType: backup.ErrInvalidTarPath,
		},
		{
			name:    "parent directory traversal",
			header:  &tar.Header{Name: "../../../etc/passwd", Typeflag: tar.TypeReg},
			wantErr: true, errType: backup.ErrInvalidTarPath,
		},
		{
			name:    "embedded parent traversal",
			header:  &tar.Header{Name: "resources/../../etc/passwd", Typeflag: tar.TypeReg},
			wantErr: true, errType: backup.ErrInvalidTarPath,
		},
		{
			name:    "double dot only",
			header:  &tar.Header{Name: "..", Typeflag: tar.TypeReg},
			wantErr: true, errType: backup.ErrInvalidTarPath,
		},
	}
}

func specialTypeTestCases() []validateTarEntryTest {
	return []validateTarEntryTest{
		{
			name: "symlink",
			header: &tar.Header{
				Name:     "link.yaml",
				Typeflag: tar.TypeSymlink,
				Linkname: tEtcPasswd,
			},
			wantErr: true, errType: backup.ErrSymlinkInArchive,
		},
		{
			name:    "hard link",
			header:  &tar.Header{Name: "link.yaml", Typeflag: tar.TypeLink, Linkname: "other.yaml"},
			wantErr: true, errType: backup.ErrSymlinkInArchive,
		},
		{
			name:    "char device",
			header:  &tar.Header{Name: "dev", Typeflag: tar.TypeChar},
			wantErr: true, errType: backup.ErrInvalidTarPath,
		},
		{
			name:    "block device",
			header:  &tar.Header{Name: "dev", Typeflag: tar.TypeBlock},
			wantErr: true, errType: backup.ErrInvalidTarPath,
		},
		{
			name:    "FIFO",
			header:  &tar.Header{Name: "fifo", Typeflag: tar.TypeFifo},
			wantErr: true, errType: backup.ErrInvalidTarPath,
		},
	}
}

func runValidateTarEntryTests(
	t *testing.T, destDir string, tests []validateTarEntryTest,
) {
	t.Helper()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := backup.ValidateTarEntry(test.header, destDir)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				if test.errType != nil &&
					!errors.Is(err, test.errType) {
					t.Errorf(
						"expected error wrapping %v, got %v",
						test.errType, err,
					)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateTarEntry(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()

	runValidateTarEntryTests(t, destDir, pathTraversalTestCases())
	runValidateTarEntryTests(t, destDir, specialTypeTestCases())
}

func TestValidateTarEntry_NestedRegularFile(t *testing.T) {
	t.Parallel()

	header := &tar.Header{
		Name:     "backup/namespaces/default/pods.yaml",
		Typeflag: tar.TypeReg,
	}

	path, err := backup.ValidateTarEntry(header, "/tmp/dest")
	require.NoError(t, err)
	assert.Contains(t, path, "pods.yaml")
}

func TestValidateTarEntry_DotSlashPrefix(t *testing.T) {
	t.Parallel()

	header := &tar.Header{
		Name:     "./backup/file.yaml",
		Typeflag: tar.TypeReg,
	}

	_, err := backup.ValidateTarEntry(header, "/tmp/dest")
	assert.NoError(t, err)
}

func TestValidateTarEntry_TrailingDotDot(t *testing.T) {
	t.Parallel()

	header := &tar.Header{
		Name:     "backup/../../../etc/shadow",
		Typeflag: tar.TypeReg,
	}

	_, err := backup.ValidateTarEntry(header, "/tmp/dest")
	require.Error(t, err)
	assert.ErrorIs(t, err, backup.ErrInvalidTarPath)
}

func TestAllLinesContain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   string
		substr   string
		expected bool
	}{
		{
			name:     "all lines match",
			output:   "error: already exists\nerror: already exists\n",
			substr:   tAlreadyExists,
			expected: true,
		},
		{
			name:     "one line does not match",
			output:   "error: already exists\nerror: forbidden\n",
			substr:   tAlreadyExists,
			expected: false,
		},
		{
			name:     "empty string",
			output:   "",
			substr:   tAlreadyExists,
			expected: false,
		},
		{
			name:     "whitespace only",
			output:   "   \n  \n",
			substr:   tAlreadyExists,
			expected: false,
		},
		{
			name:     "single matching line",
			output:   "resource already exists",
			substr:   tAlreadyExists,
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := backup.AllLinesContain(
				test.output, test.substr,
			)
			if result != test.expected {
				t.Errorf(
					"backup.AllLinesContain() = %v, want %v",
					result, test.expected,
				)
			}
		})
	}
}

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
			output:   tAlreadyExists,
			substr:   tAlreadyExists,
			expected: true,
		},
		{
			name:     "line with surrounding whitespace",
			output:   "  already exists  \n",
			substr:   tAlreadyExists,
			expected: true,
		},
		{
			name:     "mixed matching and non-matching",
			output:   "already exists\nother error",
			substr:   tAlreadyExists,
			expected: false,
		},
		{
			name:     "completely empty output",
			output:   "",
			substr:   tAlreadyExists,
			expected: false,
		},
		{
			name:     "all empty lines",
			output:   "\n\n\n",
			substr:   tAlreadyExists,
			expected: false,
		},
		{
			name:     "multiple all-matching lines",
			output:   "error: resource already exists\nerror: configmap already exists\nerror: secret already exists",
			substr:   tAlreadyExists,
			expected: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := backup.AllLinesContain(testCase.output, testCase.substr)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestAllLinesContain_OnlyEmptyLines(t *testing.T) {
	t.Parallel()

	got := backup.AllLinesContain("  \n  \n  ", "anything")
	assert.False(t, got)
}

func TestAllLinesContain_MultilineMatch(t *testing.T) {
	t.Parallel()

	got := backup.AllLinesContain(
		"error: already exists\nwarning: already exists\ninfo: already exists",
		tAlreadyExists,
	)
	assert.True(t, got)
}

func TestDeriveBackupName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tar.gz extension",
			input:    "/path/to/my-backup.tar.gz",
			expected: tBackupName,
		},
		{
			name:     "tgz extension",
			input:    "/path/to/backup.tgz",
			expected: "backup",
		},
		{
			name:     "no matching extension",
			input:    "/path/to/backup.zip",
			expected: "backup.zip",
		},
		{
			name:     "simple filename",
			input:    "cluster-backup.tar.gz",
			expected: tClusterBackup,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := backup.DeriveBackupName(test.input)
			if result != test.expected {
				t.Errorf(
					"backup.DeriveBackupName() = %q, want %q",
					result, test.expected,
				)
			}
		})
	}
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
			expected: tClusterBackup,
		},
		{
			name:     "tgz with path",
			input:    "/backups/cluster-backup.tgz",
			expected: tClusterBackup,
		},
		{
			name:     "simple filename",
			input:    "my-backup.tar.gz",
			expected: tBackupName,
		},
		{
			name:     "no extension",
			input:    tBackupName,
			expected: tBackupName,
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

			result := backup.DeriveBackupName(testCase.input)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestDeriveBackupName_OnlyExtension(t *testing.T) {
	t.Parallel()

	got := backup.DeriveBackupName(".tar.gz")
	assert.Empty(t, got)
}

func TestDeriveBackupName_NoDirectory(t *testing.T) {
	t.Parallel()

	got := backup.DeriveBackupName("simple.tgz")
	assert.Equal(t, "simple", got)
}

func TestAddLabelsToDocument(t *testing.T) {
	t.Parallel()

	for _, test := range addLabelsTestCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := backup.AddLabelsToDocument(
				test.doc, test.backupName, test.restoreName,
			)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, label := range test.wantLabels {
				if !strings.Contains(result, label) {
					t.Errorf("result should contain label %q", label)
				}
			}
		})
	}
}

type addLabelsTestCase struct {
	name        string
	doc         string
	backupName  string
	restoreName string
	wantLabels  []string
	wantErr     bool
}

func addLabelsTestCases() []addLabelsTestCase {
	return []addLabelsTestCase{
		{
			name: "adds labels to document without existing labels",
			doc: "apiVersion: v1\nkind: Pod\nmetadata:\n" +
				"  name: test-pod\n  namespace: default\n",
			backupName:  tBackupName,
			restoreName: "restore-123",
			wantLabels: []string{
				"ksail.io/backup-name",
				"ksail.io/restore-name",
			},
		},
		{
			name: "preserves existing labels",
			doc: "apiVersion: v1\nkind: Pod\nmetadata:\n" +
				"  name: test-pod\n  labels:\n    app: nginx\n",
			backupName:  "backup-1",
			restoreName: "restore-1",
			wantLabels: []string{
				tLabelApp,
				"ksail.io/backup-name",
				"ksail.io/restore-name",
			},
		},
		{
			name:        "returns original for empty doc",
			doc:         "\n",
			backupName:  "backup",
			restoreName: "restore",
			wantErr:     false,
		},
	}
}

func TestAddLabelsToDocument_PreservesExistingLabels(t *testing.T) {
	t.Parallel()

	doc := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\n  labels:\n    app: foo\n    env: prod"
	got, err := backup.AddLabelsToDocument(doc, "backup-1", "restore-1")
	require.NoError(t, err)

	// Original labels should still be present
	assert.Contains(t, got, tLabelApp)
	assert.Contains(t, got, "env")
	// New labels should be added
	assert.Contains(t, got, "ksail.io/backup-name")
	assert.Contains(t, got, "ksail.io/restore-name")
}

func TestSplitYAMLDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "single document",
			content:  "apiVersion: v1\nkind: Pod\n",
			expected: 1,
		},
		{
			name:     "two documents",
			content:  "apiVersion: v1\nkind: Pod\n---\napiVersion: v1\nkind: Service\n",
			expected: 2,
		},
		{
			name:     "three documents",
			content:  "kind: A\n---\nkind: B\n---\nkind: C\n",
			expected: 3,
		},
		{
			name:     "empty content",
			content:  "",
			expected: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := backup.SplitYAMLDocuments(test.content)
			if len(result) != test.expected {
				t.Errorf(
					"backup.SplitYAMLDocuments() returned %d docs, want %d",
					len(result), test.expected,
				)
			}
		})
	}
}

func TestSplitYAMLDocuments_MultipleTrailingSeparators(t *testing.T) {
	t.Parallel()

	docs := backup.SplitYAMLDocuments("a: 1\n---\n---\n---\nb: 2")
	require.Len(t, docs, 2)
	assert.Contains(t, docs[0], "a: 1")
	assert.Contains(t, docs[1], "b: 2")
}

func TestSplitYAMLDocuments_OnlySeparators(t *testing.T) {
	t.Parallel()

	docs := backup.SplitYAMLDocuments("---\n---\n---")
	assert.Empty(t, docs)
}

func TestInjectRestoreLabels(t *testing.T) {
	t.Parallel()

	content := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\n"

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "test.yaml")

	err := os.WriteFile(inputPath, []byte(content), backup.FilePerm)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	labeledPath, err := backup.InjectRestoreLabels(
		inputPath, tBackupName, "restore-42",
	)
	if err != nil {
		t.Fatalf("backup.InjectRestoreLabels() error: %v", err)
	}

	defer func() { _ = os.Remove(labeledPath) }()

	data, err := os.ReadFile(labeledPath) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("failed to read labeled file: %v", err)
	}

	result := string(data)

	if !strings.Contains(result, "ksail.io/backup-name") {
		t.Error("labeled file should contain ksail.io/backup-name")
	}

	if !strings.Contains(result, "ksail.io/restore-name") {
		t.Error("labeled file should contain ksail.io/restore-name")
	}

	if !strings.Contains(result, tBackupName) {
		t.Error("labeled file should contain backup name value")
	}

	if !strings.Contains(result, "restore-42") {
		t.Error("labeled file should contain restore name value")
	}
}

func TestIsEmptyYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty file", "", true},
		{"only separators", "---\n---\n", true},
		{"only whitespace", "   \n  \n", true},
		{"mixed separators and whitespace", "---\n\n  \n---", true},
		{"has content", "apiVersion: v1\nkind: Pod", false},
		{"separator with content", "---\napiVersion: v1", false},
		{"single non-empty line", "hello", false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "test.yaml")
			require.NoError(t, os.WriteFile(path, []byte(testCase.content), 0o600))

			got := backup.IsEmptyYAML(path)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestIsEmptyYAML_NonexistentFile(t *testing.T) {
	t.Parallel()

	got := backup.IsEmptyYAML("/nonexistent/path/file.yaml")
	assert.False(t, got)
}

// TestReadBackupMetadata verifies error paths and happy path of readBackupMetadata.
func TestReadBackupMetadata( //nolint:funlen // Covers multiple distinct error and success paths
	t *testing.T,
) {
	t.Parallel()

	t.Run("returns error when metadata file is missing", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		_, err := backup.ReadBackupMetadata(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup metadata")
	})

	t.Run("returns error when metadata is not valid JSON", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		metaPath := filepath.Join(tmpDir, "backup-metadata.json")
		err := os.WriteFile(metaPath, []byte("{not valid json"), 0o600)
		require.NoError(t, err)

		_, err = backup.ReadBackupMetadata(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse backup metadata")
	})

	t.Run("returns metadata when file is valid JSON", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		meta := &backup.BackupMetadata{
			Version:       "v1",
			ClusterName:   "test",
			ResourceCount: 7,
		}

		data, err := json.Marshal(meta)
		require.NoError(t, err)

		metaPath := filepath.Join(tmpDir, "backup-metadata.json")
		err = os.WriteFile(metaPath, data, 0o600)
		require.NoError(t, err)

		result, err := backup.ReadBackupMetadata(tmpDir)
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

		result, err := backup.ReadBackupMetadata(tmpDir)
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

		_, _, err := backup.ExtractBackupArchive(
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

		_, _, err = backup.ExtractBackupArchive(badFile)
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

		_, _, err = backup.ExtractBackupArchive(archivePath)
		require.Error(t, err)
	})

	t.Run("valid tar.gz without metadata returns error", func(t *testing.T) {
		t.Parallel()

		archivePath := createArchiveWithoutMetadata(t)

		_, _, err := backup.ExtractBackupArchive(archivePath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup metadata")
	})
}

// TestExtractBackupArchive_SecurityGuards validates that malicious tar entries
// (path traversal, symlinks, hard links) are rejected when processed through
// the full extractBackupArchive pipeline.
func TestExtractBackupArchive_SecurityGuards(t *testing.T) {
	t.Parallel()

	validMeta := `{"version":"v1","clusterName":"test-cluster","resourceCount":1}`

	t.Run("path traversal entry rejected", func(t *testing.T) {
		t.Parallel()

		archivePath := createMaliciousArchive(t, validMeta, tar.Header{
			Name:     "../../../etc/passwd",
			Typeflag: tar.TypeReg,
			Size:     int64(len("root:x:0:0")),
			Mode:     0o600,
		}, []byte("root:x:0:0"))

		_, _, err := backup.ExtractBackupArchive(archivePath)
		require.Error(t, err)
		assert.ErrorIs(t, err, backup.ErrInvalidTarPath,
			"expected backup.ErrInvalidTarPath, got: %v", err)
	})

	t.Run("symlink entry rejected", func(t *testing.T) {
		t.Parallel()

		archivePath := createMaliciousArchive(t, validMeta, tar.Header{
			Name:     "resources/namespaces/evil.yaml",
			Typeflag: tar.TypeSymlink,
			Linkname: tEtcPasswd,
			Mode:     0o777,
		}, nil)

		_, _, err := backup.ExtractBackupArchive(archivePath)
		require.Error(t, err)
		assert.ErrorIs(t, err, backup.ErrSymlinkInArchive,
			"expected backup.ErrSymlinkInArchive, got: %v", err)
	})

	t.Run("hard link entry rejected", func(t *testing.T) {
		t.Parallel()

		archivePath := createMaliciousArchive(t, validMeta, tar.Header{
			Name:     "resources/namespaces/hardlink.yaml",
			Typeflag: tar.TypeLink,
			Linkname: tPodsYAMLPath,
			Mode:     0o600,
		}, nil)

		_, _, err := backup.ExtractBackupArchive(archivePath)
		require.Error(t, err)
		assert.ErrorIs(t, err, backup.ErrSymlinkInArchive,
			"expected backup.ErrSymlinkInArchive, got: %v", err)
	})
}

// createMaliciousArchive builds a tar.gz with valid metadata followed by a
// caller-supplied malicious tar entry. If content is nil the entry is written
// as a header-only entry (appropriate for symlinks/hard links).
func createMaliciousArchive(
	t *testing.T,
	metaJSON string,
	malicious tar.Header,
	content []byte,
) string {
	t.Helper()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "malicious.tar.gz")

	f, err := os.Create(archivePath) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	gzipWriter := gzip.NewWriter(f)
	tarWriter := tar.NewWriter(gzipWriter)

	// 1. Valid backup-metadata.json so the archive passes initial parsing.
	addTarEntry(t, tarWriter, "backup-metadata.json", []byte(metaJSON))

	// 2. resources/ directory entry.
	err = tarWriter.WriteHeader(&tar.Header{
		Name:     tResourcesDir,
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	})
	require.NoError(t, err)

	// 3. The malicious entry.
	err = tarWriter.WriteHeader(&malicious)
	require.NoError(t, err)

	if content != nil {
		_, err = tarWriter.Write(content)
		require.NoError(t, err)
	}

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())

	return archivePath
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
		Name:     tPodsYAMLPath,
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

	tmpDir, meta, err := backup.ExtractBackupArchive(archivePath)
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
		Name:     tResourcesDir,
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	})
	require.NoError(t, err)

	// Add a YAML resource file.
	addTarEntry(t, tarWriter, tPodsYAMLPath, []byte("apiVersion: v1\nkind: Pod\n"))

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())

	return archivePath
}

// addTarEntry writes a regular file entry into the tar writer.
func addTarEntry(t *testing.T, tarWriter *tar.Writer, name string, content []byte) {
	t.Helper()

	err := tarWriter.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Size:     int64(len(content)),
		Mode:     0o600,
	})
	require.NoError(t, err)

	_, err = tarWriter.Write(content)
	require.NoError(t, err)
}

// TestClassifyRestoreError_FallbackToErrMsg verifies that classifyRestoreError
// falls back to err.Error() when stderr is empty, so "already exists" errors
// routed through BehaviorOnFatal (not stderr) are correctly suppressed.
//
//nolint:funlen,err113 // Table-driven test with comprehensive cases; test errors are intentionally dynamic
func TestClassifyRestoreError_FallbackToErrMsg(t *testing.T) {
	t.Parallel()

	// Sentinel errors used as test inputs for classifyRestoreError.
	var (
		errExitStatus1     = errors.New("exit status 1")
		errDaemonSetExists = errors.New(
			"Error from server (AlreadyExists): daemonsets.apps \"svclb-traefik\" already exists",
		)
		errMultipleAlreadyExist = errors.New(
			"daemonsets.apps \"svclb-traefik\" already exists\n" +
				"jobs.batch \"helm-install-traefik\" already exists",
		)
		errMixedExistsAndOther = errors.New(
			"daemonsets.apps \"svclb-traefik\" already exists\n" +
				"connection refused",
		)
		errConnectionRefused = errors.New("connection refused")
		errAlreadyExists     = errors.New(tAlreadyExists)
	)

	tests := []struct {
		name      string
		err       error
		stderr    string
		policy    string
		expectNil bool
	}{
		{
			name:      "nil error returns nil",
			err:       nil,
			stderr:    "",
			policy:    tPolicyNone,
			expectNil: true,
		},
		{
			name:      "already exists in stderr with policy none",
			err:       errExitStatus1,
			stderr:    "Error from server (AlreadyExists): resource already exists",
			policy:    tPolicyNone,
			expectNil: true,
		},
		{
			name:      "already exists in err.Error() with empty stderr",
			err:       errDaemonSetExists,
			stderr:    "",
			policy:    tPolicyNone,
			expectNil: true,
		},
		{
			name:      "already exists in err.Error() with whitespace-only stderr",
			err:       errDaemonSetExists,
			stderr:    "\n",
			policy:    tPolicyNone,
			expectNil: true,
		},
		{
			name:      "multiple already exists lines in err.Error()",
			err:       errMultipleAlreadyExist,
			stderr:    "",
			policy:    tPolicyNone,
			expectNil: true,
		},
		{
			name:      "mixed error in err.Error() with empty stderr",
			err:       errMixedExistsAndOther,
			stderr:    "",
			policy:    tPolicyNone,
			expectNil: false,
		},
		{
			name:      "real error with empty stderr",
			err:       errConnectionRefused,
			stderr:    "",
			policy:    tPolicyNone,
			expectNil: false,
		},
		{
			name:      "already exists with policy update does not suppress",
			err:       errAlreadyExists,
			stderr:    "",
			policy:    "update",
			expectNil: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := backup.ClassifyRestoreError(
				testCase.err, testCase.stderr, testCase.policy,
			)
			if testCase.expectNil {
				assert.NoError(t, result)
			} else {
				assert.Error(t, result)
			}
		})
	}
}

//nolint:err113 // test error is intentionally dynamic
func TestClassifyRestoreError_AlreadyExistsFromErrMsg(t *testing.T) {
	t.Parallel()

	// When stderr is empty but error message says "already exists", should be nil with "none" policy
	err := backup.ClassifyRestoreError(
		errors.New("resource already exists"),
		"",
		tPolicyNone,
	)
	assert.NoError(t, err)
}

//nolint:err113 // test error is intentionally dynamic
func TestClassifyRestoreError_EmptyStderrWithUpdatePolicy(t *testing.T) {
	t.Parallel()

	err := backup.ClassifyRestoreError(
		errors.New("some error"),
		"",
		"update",
	)
	assert.Error(t, err)
}
