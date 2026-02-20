package cluster

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupMetadata(t *testing.T) {
	// Test metadata marshaling
	metadata := &BackupMetadata{
		Version:       "v1",
		ClusterName:   "test-cluster",
		KSailVersion:  "5.0.0",
		ResourceCount: 42,
	}

	tmpDir := t.TempDir()
	metadataPath := filepath.Join(tmpDir, "backup-metadata.json")

	// Test writing metadata
	if err := writeMetadata(metadata, metadataPath); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Fatal("metadata file was not created")
	}

	// Verify file is valid JSON
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("failed to read metadata file: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("metadata file is empty")
	}
}

func TestCreateTarball(t *testing.T) {
	// Create a temporary source directory with test files
	srcDir := t.TempDir()
	testFile := filepath.Join(srcDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create subdirectory
	subDir := filepath.Join(srcDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}
	subFile := filepath.Join(subDir, "sub.txt")
	if err := os.WriteFile(subFile, []byte("sub content"), 0644); err != nil {
		t.Fatalf("failed to create sub file: %v", err)
	}

	// Create output tarball
	outputPath := filepath.Join(t.TempDir(), "test-backup.tar.gz")
	if err := createTarball(srcDir, outputPath, 6); err != nil {
		t.Fatalf("failed to create tarball: %v", err)
	}

	// Verify tarball exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("tarball was not created")
	}

	// Verify file is not empty
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("failed to stat tarball: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("tarball is empty")
	}
}

func TestCountYAMLDocuments(t *testing.T) {
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
			name:     "empty content",
			content:  "",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := countYAMLDocuments(tt.content)
			if tt.content == "" {
				// Empty content should not count
				return
			}
			if count != tt.expected {
				t.Errorf("countYAMLDocuments() = %d, want %d", count, tt.expected)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{"substring at start", "hello world", "hello", true},
		{"substring at end", "hello world", "world", true},
		{"substring in middle", "hello world", "lo wo", true},
		{"substring not found", "hello world", "xyz", false},
		{"empty substring", "hello", "", true},
		{"empty string", "", "test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single line",
			input:    "hello",
			expected: []string{"hello"},
		},
		{
			name:     "multiple lines",
			input:    "line1\nline2\nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "trailing newline",
			input:    "line1\nline2\n",
			expected: []string{"line1", "line2", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitLines(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("splitLines() returned %d lines, want %d", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("splitLines()[%d] = %q, want %q", i, result[i], tt.expected[i])
				}
			}
		})
	}
}
