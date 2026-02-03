package chat //nolint:testpackage // white-box tests for unexported functions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDocFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"markdown file", "docs/readme.md", true},
		{"mdx file", "docs/guide.mdx", true},
		{"txt file", "docs/notes.txt", false},
		{"go file", "main.go", false},
		{"no extension", "docs/file", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := isDocFile(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestIsIndexFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"index.md", "docs/index.md", true},
		{"index.mdx", "docs/index.mdx", true},
		{"readme.md", "docs/readme.md", false},
		{"guide.md", "docs/guide.md", false},
		{"regular file", "docs/api.md", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := isIndexFile(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExtractTitleFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"simple filename", "guide.md", "Guide"},
		{"path with dir", "docs/api.md", "Api"},
		{"hyphenated", "getting-started.md", "Getting Started"},
		{"deep path", "a/b/c/readme.md", "Readme"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := extractTitleFromPath(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}
