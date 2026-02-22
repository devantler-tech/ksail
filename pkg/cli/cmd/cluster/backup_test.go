package cluster

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupMetadata(t *testing.T) {
	t.Parallel()

	metadata := &BackupMetadata{
		Version:       "v1",
		ClusterName:   "test-cluster",
		KSailVersion:  "5.0.0",
		ResourceCount: 42,
	}

	tmpDir := t.TempDir()
	metadataPath := filepath.Join(tmpDir, "backup-metadata.json")

	err := writeMetadata(metadata, metadataPath)
	if err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	_, statErr := os.Stat(metadataPath)
	if os.IsNotExist(statErr) {
		t.Fatal("metadata file was not created")
	}

	data, err := os.ReadFile(metadataPath) //nolint:gosec // test file path
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

	err := os.WriteFile(testFile, []byte("test content"), filePerm)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	subDir := filepath.Join(srcDir, "subdir")

	err = os.MkdirAll(subDir, dirPerm)
	if err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	subFile := filepath.Join(subDir, "sub.txt")

	err = os.WriteFile(subFile, []byte("sub content"), filePerm)
	if err != nil {
		t.Fatalf("failed to create sub file: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "test-backup.tar.gz")

	err = createTarball(srcDir, outputPath, 6)
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			count := countYAMLDocuments(test.content)
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

			result := filterExcludedTypes(test.types, test.exclude)
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
	metadata := &BackupMetadata{
		Version:       "v1",
		ClusterName:   "roundtrip-cluster",
		KSailVersion:  "5.0.0",
		ResourceCount: 10,
	}

	metadataPath := filepath.Join(srcDir, "backup-metadata.json")

	err := writeMetadata(metadata, metadataPath)
	if err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "test.tar.gz")

	err = createTarball(srcDir, archivePath, 6)
	if err != nil {
		t.Fatalf("failed to create tarball: %v", err)
	}

	tmpDir, restored, err := extractBackupArchive(archivePath)
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
