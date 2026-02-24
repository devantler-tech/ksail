package cluster_test

import (
	"archive/tar"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cluster "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
)

func TestBackupMetadata(t *testing.T) {
	t.Parallel()

	metadata := &cluster.BackupMetadata{
		Version:       "v1",
		ClusterName:   "test-cluster",
		KSailVersion:  "5.0.0",
		ResourceCount: 42,
	}

	tmpDir := t.TempDir()
	metadataPath := filepath.Join(tmpDir, "backup-metadata.json")

	err := cluster.ExportWriteMetadata(metadata, metadataPath)
	if err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	_, statErr := os.Stat(metadataPath)
	if os.IsNotExist(statErr) {
		t.Fatal("metadata file was not created")
	}

	data, err := os.ReadFile(metadataPath) //nolint:gosec // test-controlled path from t.TempDir()
	if err != nil {
		t.Fatalf("failed to read metadata file: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("metadata file is empty")
	}
}

func TestCreateTarball(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()

	testFile := filepath.Join(srcDir, "test.txt")

	err := os.WriteFile(testFile, []byte("test content"), cluster.ExportFilePerm)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	subDir := filepath.Join(srcDir, "subdir")

	err = os.MkdirAll(subDir, cluster.ExportDirPerm)
	if err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	subFile := filepath.Join(subDir, "sub.txt")

	err = os.WriteFile(subFile, []byte("sub content"), cluster.ExportFilePerm)
	if err != nil {
		t.Fatalf("failed to create sub file: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "test-backup.tar.gz")

	err = cluster.ExportCreateTarball(srcDir, outputPath, 6)
	if err != nil {
		t.Fatalf("failed to create tarball: %v", err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("failed to stat tarball: %v", err)
	}

	if info.Size() == 0 {
		t.Fatal("tarball is empty")
	}
}

func TestCountYAMLDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "single document",
			content:  "kind: Pod\nmetadata:\n  name: test",
			expected: 1,
		},
		{
			name:     "multiple documents",
			content:  "kind: Pod\n---\nkind: Service\n---\nkind: Deployment",
			expected: 3,
		},
		{
			name:     "no kind lines returns 1",
			content:  "metadata:\n  name: test",
			expected: 1,
		},
		{
			name: "kubectl list output",
			content: "apiVersion: v1\nkind: PodList\nmetadata:\n" +
				"items:\n- apiVersion: v1\n  kind: Pod\n  metadata:\n" +
				"    name: pod1\n- apiVersion: v1\n  kind: Pod\n  metadata:\n" +
				"    name: pod2\n",
			expected: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			count := cluster.ExportCountYAMLDocuments(test.content)
			if count != test.expected {
				t.Errorf(
					"countYAMLDocuments() = %d, want %d",
					count, test.expected,
				)
			}
		})
	}
}

func TestFilterExcludedTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		types       []string
		exclude     []string
		expectedLen int
	}{
		{
			name:        "no exclusions",
			types:       []string{"pods", "services", "deployments"},
			exclude:     []string{},
			expectedLen: 3,
		},
		{
			name:        "exclude one",
			types:       []string{"pods", "services", "deployments"},
			exclude:     []string{"pods"},
			expectedLen: 2,
		},
		{
			name:        "exclude all",
			types:       []string{"pods"},
			exclude:     []string{"pods"},
			expectedLen: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.ExportFilterExcludedTypes(test.types, test.exclude)
			if len(result) != test.expectedLen {
				t.Errorf(
					"filterExcludedTypes() returned %d items, want %d",
					len(result), test.expectedLen,
				)
			}
		})
	}
}

func TestExtractAndReadMetadata(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	metadata := &cluster.BackupMetadata{
		Version:       "v1",
		ClusterName:   "roundtrip-cluster",
		KSailVersion:  "5.0.0",
		ResourceCount: 10,
	}

	metadataPath := filepath.Join(srcDir, "backup-metadata.json")

	err := cluster.ExportWriteMetadata(metadata, metadataPath)
	if err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "test.tar.gz")

	err = cluster.ExportCreateTarball(srcDir, archivePath, 6)
	if err != nil {
		t.Fatalf("failed to create tarball: %v", err)
	}

	tmpDir, restored, err := cluster.ExportExtractBackupArchive(archivePath)
	if err != nil {
		t.Fatalf("failed to extract backup archive: %v", err)
	}

	defer func() { _ = os.RemoveAll(tmpDir) }()

	if restored.Version != "v1" {
		t.Errorf("Version = %q, want %q", restored.Version, "v1")
	}

	if restored.ClusterName != "roundtrip-cluster" {
		t.Errorf(
			"ClusterName = %q, want %q",
			restored.ClusterName, "roundtrip-cluster",
		)
	}

	if restored.ResourceCount != 10 {
		t.Errorf(
			"ResourceCount = %d, want %d",
			restored.ResourceCount, 10,
		)
	}
}

func TestSanitizeYAMLOutput(t *testing.T) {
	t.Parallel()

	input := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test-pod\n" +
		"  namespace: default\n  resourceVersion: \"12345\"\n" +
		"  uid: abc-123\n  managedFields:\n  - manager: kubectl\n" +
		"  creationTimestamp: \"2025-01-01T00:00:00Z\"\n" +
		"status:\n  phase: Running\nspec:\n  containers:\n  - name: nginx"

	result, err := cluster.ExportSanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("sanitizeYAMLOutput() error = %v", err)
	}

	for _, stripped := range []string{
		"resourceVersion", "uid", "managedFields",
		"creationTimestamp", "status",
	} {
		if strings.Contains(result, stripped) {
			t.Errorf("should have stripped %q", stripped)
		}
	}

	for _, preserved := range []string{
		"name: test-pod", "namespace: default",
		"kind: Pod", "apiVersion: v1",
	} {
		if !strings.Contains(result, preserved) {
			t.Errorf("should preserve %q", preserved)
		}
	}
}

func TestSanitizeYAMLOutput_nonYAML(t *testing.T) {
	t.Parallel()

	result, err := cluster.ExportSanitizeYAMLOutput("not valid yaml: [")
	if err != nil {
		t.Fatalf("sanitizeYAMLOutput() error = %v", err)
	}

	if !strings.Contains(result, "not valid yaml") {
		t.Error("should return original content for non-YAML input")
	}
}

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
			header: &tar.Header{Name: "resources/pods.yaml", Typeflag: tar.TypeReg},
		},
		{
			name:   "valid directory",
			header: &tar.Header{Name: "resources/", Typeflag: tar.TypeDir},
		},
		{
			name:    "absolute path",
			header:  &tar.Header{Name: "/etc/passwd", Typeflag: tar.TypeReg},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "parent directory traversal",
			header:  &tar.Header{Name: "../../../etc/passwd", Typeflag: tar.TypeReg},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "embedded parent traversal",
			header:  &tar.Header{Name: "resources/../../etc/passwd", Typeflag: tar.TypeReg},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "double dot only",
			header:  &tar.Header{Name: "..", Typeflag: tar.TypeReg},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
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
				Linkname: "/etc/passwd",
			},
			wantErr: true, errType: cluster.ErrSymlinkInArchive,
		},
		{
			name:    "hard link",
			header:  &tar.Header{Name: "link.yaml", Typeflag: tar.TypeLink, Linkname: "other.yaml"},
			wantErr: true, errType: cluster.ErrSymlinkInArchive,
		},
		{
			name:    "char device",
			header:  &tar.Header{Name: "dev", Typeflag: tar.TypeChar},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "block device",
			header:  &tar.Header{Name: "dev", Typeflag: tar.TypeBlock},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "FIFO",
			header:  &tar.Header{Name: "fifo", Typeflag: tar.TypeFifo},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
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

			_, err := cluster.ExportValidateTarEntry(test.header, destDir)
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
			substr:   "already exists",
			expected: true,
		},
		{
			name:     "one line does not match",
			output:   "error: already exists\nerror: forbidden\n",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "empty string",
			output:   "",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "whitespace only",
			output:   "   \n  \n",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "single matching line",
			output:   "resource already exists",
			substr:   "already exists",
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.ExportAllLinesContain(
				test.output, test.substr,
			)
			if result != test.expected {
				t.Errorf(
					"allLinesContain() = %v, want %v",
					result, test.expected,
				)
			}
		})
	}
}
