package io_test

import (
	"os/user"
	"path/filepath"
	"testing"

	iopath "github.com/devantler-tech/ksail-go/pkg/io"
)

func TestExpandHomePath(t *testing.T) {
	t.Parallel()

	usr, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "expands home prefix",
			input:    "~/some/nested/dir",
			expected: filepath.Join(usr.HomeDir, "some", "nested", "dir"),
		},
		{
			name:     "returns unchanged when no tilde - relative path",
			input:    filepath.Join("var", "tmp"),
			expected: filepath.Join("var", "tmp"),
		},
		{
			name:     "returns unchanged when no tilde - absolute path",
			input:    filepath.Join(string(filepath.Separator), "tmp", "file"),
			expected: filepath.Join(string(filepath.Separator), "tmp", "file"),
		},
		{
			name:     "tilde only unchanged",
			input:    "~",
			expected: "~",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := iopath.ExpandHomePath(testCase.input)
			if err != nil {
				t.Fatalf("ExpandHomePath returned error: %v", err)
			}

			if got != testCase.expected {
				t.Fatalf("ExpandHomePath(%q) = %q, want %q", testCase.input, got, testCase.expected)
			}
		})
	}
}
