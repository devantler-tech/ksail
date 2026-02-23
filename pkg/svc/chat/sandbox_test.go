package chat_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/chat"
)

func TestIsPathWithinDirectory(t *testing.T) {
	t.Parallel()

	// Use a real temporary directory as the root so filepath.EvalSymlinks succeeds.
	root := t.TempDir()

	// Create a subdirectory and a file inside it for realistic path resolution.
	subDir := filepath.Join(root, "sub")

	err := os.MkdirAll(subDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	nestedFile := filepath.Join(subDir, "file.txt")

	err = os.WriteFile(nestedFile, []byte("test"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	// Create a directory outside root for escape tests.
	outsideDir := t.TempDir()

	outsideFile := filepath.Join(outsideDir, "secret.txt")

	err = os.WriteFile(outsideFile, []byte("secret"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	tests := buildPathTests(root, subDir, nestedFile, outsideDir, outsideFile)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := chat.IsPathWithinDirectory(test.path, test.root)
			if got != test.want {
				t.Errorf(
					"IsPathWithinDirectory(%q, %q) = %v, want %v",
					test.path, test.root, got, test.want,
				)
			}
		})
	}
}

type pathTest struct {
	name string
	path string
	root string
	want bool
}

func buildPathTests(root, subDir, nestedFile, outsideDir, outsideFile string) []pathTest {
	return []pathTest{
		{"exact root is allowed", root, root, true},
		{"file inside root is allowed", nestedFile, root, true},
		{"subdirectory inside root is allowed", subDir, root, true},
		{
			"absolute path within root is allowed",
			filepath.Join(root, "sub", "file.txt"), root, true,
		},
		{
			"dotdot traversal escaping root is denied",
			filepath.Join(root, "..", filepath.Base(outsideDir)), root, false,
		},
		{"absolute path outside root is denied", outsideFile, root, false},
		{"empty path is denied", "", root, false},
		{"empty root is denied", nestedFile, "", false},
		{
			"nonexistent file in valid parent is allowed",
			filepath.Join(subDir, "newfile.txt"), root, true,
		},
		{
			"nonexistent file outside root is denied",
			filepath.Join(outsideDir, "newfile.txt"), root, false,
		},
	}
}

func TestIsPathWithinDirectory_symlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outsideDir := t.TempDir()

	outsideFile := filepath.Join(outsideDir, "escape.txt")

	err := os.WriteFile(outsideFile, []byte("escape"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside root that points outside root.
	symlinkPath := filepath.Join(root, "sneaky-link")

	err = os.Symlink(outsideDir, symlinkPath)
	if err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	got := chat.IsPathWithinDirectory(filepath.Join(symlinkPath, "escape.txt"), root)
	if got {
		t.Error("expected symlink escaping root to be denied, but was allowed")
	}
}
