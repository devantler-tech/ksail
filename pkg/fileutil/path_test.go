package fileutil_test

import (
	"os/user"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/fileutil"
)

func TestExpandHomePath(t *testing.T) {
	t.Parallel()

	usr, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}

	tests := []struct {
		name        string
		input       string
		expected    string
		expectAbsOf string // If set, expect an absolute path of this relative path
	}{
		{
			name:     "expands home prefix",
			input:    "~/some/nested/dir",
			expected: filepath.Join(usr.HomeDir, "some", "nested", "dir"),
		},
		{
			name:        "converts relative path to absolute",
			input:       filepath.Join("var", "tmp"),
			expectAbsOf: filepath.Join("var", "tmp"),
		},
		{
			name:     "returns unchanged when already absolute",
			input:    filepath.Join(string(filepath.Separator), "tmp", "file"),
			expected: filepath.Join(string(filepath.Separator), "tmp", "file"),
		},
		{
			name:        "tilde only converted to absolute",
			input:       "~",
			expectAbsOf: "~",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := fileutil.ExpandHomePath(testCase.input)
			if err != nil {
				t.Fatalf("ExpandHomePath returned error: %v", err)
			}

			if testCase.expectAbsOf != "" {
				// Verify it's the absolute version of the relative path
				expected, err := filepath.Abs(testCase.expectAbsOf)
				if err != nil {
					t.Fatalf("failed to get absolute path: %v", err)
				}

				if got != expected {
					t.Fatalf("ExpandHomePath(%q) = %q, want %q", testCase.input, got, expected)
				}
			} else if got != testCase.expected {
				t.Fatalf("ExpandHomePath(%q) = %q, want %q", testCase.input, got, testCase.expected)
			}
		})
	}
}
