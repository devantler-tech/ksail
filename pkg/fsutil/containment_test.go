package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

type containmentCase struct {
	name string
	path string
	root string
	want bool
}

func buildContainmentCases(
	root, subDir, nestedFile, outsideDir, outsideFile string,
) []containmentCase {
	return []containmentCase{
		{"exact root is allowed", root, root, true},
		{"file inside root is allowed", nestedFile, root, true},
		{"subdirectory inside root is allowed", subDir, root, true},
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

func TestIsPathWithinDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
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

	outsideDir := t.TempDir()

	outsideFile := filepath.Join(outsideDir, "secret.txt")

	err = os.WriteFile(outsideFile, []byte("secret"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range buildContainmentCases(root, subDir, nestedFile, outsideDir, outsideFile) {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := fsutil.IsPathWithinDirectory(test.path, test.root)
			if got != test.want {
				t.Errorf(
					"IsPathWithinDirectory(%q, %q) = %v, want %v",
					test.path, test.root, got, test.want,
				)
			}
		})
	}
}

// TestIsPathWithinDirectory_siblingPrefix guards against the classic
// "/base_evil" sibling that a naive HasPrefix("/base") check would wrongly
// accept.
func TestIsPathWithinDirectory_siblingPrefix(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()

	base := filepath.Join(parent, "base")

	err := os.MkdirAll(base, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	sibling := filepath.Join(parent, "base_evil")

	err = os.MkdirAll(sibling, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	if fsutil.IsPathWithinDirectory(sibling, base) {
		t.Error("expected sibling directory sharing a name prefix to be denied")
	}
}

func TestIsPathWithinDirectory_symlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outsideDir := t.TempDir()

	outsideFile := filepath.Join(outsideDir, "escape.txt")

	err := os.WriteFile(outsideFile, []byte("escape"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	symlinkPath := filepath.Join(root, "sneaky-link")

	err = os.Symlink(outsideDir, symlinkPath)
	if err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	if fsutil.IsPathWithinDirectory(filepath.Join(symlinkPath, "escape.txt"), root) {
		t.Error("expected symlink escaping root to be denied, but was allowed")
	}
}
