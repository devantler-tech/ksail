package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

func TestExpandHomePath(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get user home directory: %v", err)
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
			expected: filepath.Join(home, "some", "nested", "dir"),
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

			got, err := fsutil.ExpandHomePath(testCase.input)
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

// TestExpandHomePathRespectsHOMEEnv guards the regression that destroyed the
// developer's real ~/.kube/config: ExpandHomePath must resolve "~/" against
// $HOME (os.UserHomeDir), not the OS user database. Tests redirect $HOME to a
// temporary directory; if ~ expansion ignores that override, kubeconfig
// cleanup and other home-derived writes escape the test sandbox and mutate the
// real configuration.
func TestExpandHomePathRespectsHOMEEnv(t *testing.T) {
	// Not parallel: mutates the process environment via t.Setenv.
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome) // Windows equivalent consulted by os.UserHomeDir.

	got, err := fsutil.ExpandHomePath("~/.kube/config")
	if err != nil {
		t.Fatalf("ExpandHomePath returned error: %v", err)
	}

	want := filepath.Join(tempHome, ".kube", "config")
	if got != want {
		t.Fatalf(
			"ExpandHomePath(~/.kube/config) = %q, want %q; ~ must honor $HOME so tests can redirect home-derived paths",
			got,
			want,
		)
	}
}

// TestExpandHomePathErrorsWhenHomeUnset covers the failure branch taken when
// the home directory cannot be resolved (os.UserHomeDir returns an error).
func TestExpandHomePathErrorsWhenHomeUnset(t *testing.T) {
	// Not parallel: mutates the process environment via t.Setenv.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "") // Windows equivalent consulted by os.UserHomeDir.

	_, err := fsutil.ExpandHomePath("~/anything")
	if err == nil {
		t.Fatal("ExpandHomePath(~/...) with no home directory set should return an error")
	}
}
