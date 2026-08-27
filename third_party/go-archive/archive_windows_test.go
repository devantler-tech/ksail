//go:build windows

package archive

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
)

func TestCopyFileWithInvalidDest(t *testing.T) {
	// TODO Windows: This is currently failing. Not sure what has
	// recently changed in CopyWithTar as used to pass. Further investigation
	// is required.
	t.Skip("Currently fails")
	folder, err := os.MkdirTemp("", "docker-archive-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(folder)
	dest := "c:dest"
	srcFolder := filepath.Join(folder, "src")
	src := filepath.Join(folder, "src", "src")
	if err := os.MkdirAll(srcFolder, 0o740); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("content"), 0o777); err != nil {
		t.Fatal(err)
	}
	err = defaultCopyWithTar(src, dest)
	if err == nil {
		t.Fatalf("archiver.CopyWithTar should throw an error on invalid dest.")
	}
}

func TestCanonicalTarName(t *testing.T) {
	cases := []struct {
		in       string
		isDir    bool
		expected string
	}{
		{"foo", false, "foo"},
		{"foo", true, "foo/"},
		{`foo\bar`, false, "foo/bar"},
		{`foo\bar`, true, "foo/bar/"},
	}
	for _, v := range cases {
		if canonicalTarName(v.in, v.isDir) != v.expected {
			t.Fatalf("wrong canonical tar name. expected:%s got:%s", v.expected, canonicalTarName(v.in, v.isDir))
		}
	}
}

func TestChmodTarEntry(t *testing.T) {
	cases := []struct {
		in, expected int64
	}{
		{0o000, 0o111},
		{0o777, 0o755},
		{0o644, 0o755},
		{0o755, 0o755},
		{0o444, 0o555},
	}
	for _, v := range cases {
		if out := chmodTarEntry(v.in); out != v.expected {
			t.Fatalf("wrong chmod: expected=%#o got=%#o", v.expected, out)
		}
	}
}

// TestChtimesSetsCreationTime verifies that updating file timestamps also
// sets the Windows creation time from the modification time.
func TestChtimesSetsCreationTime(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "file")
	assert.NilError(t, os.WriteFile(file, []byte("hello toto"), 0o644))

	root, err := os.OpenRoot(tmpDir)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	aTime := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC)
	mTime := time.Date(2001, time.February, 3, 4, 5, 6, 0, time.UTC)
	assert.NilError(t, chtimes(root, "file", aTime, mTime))

	fi, err := root.Stat("file")
	assert.NilError(t, err)

	data, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	assert.Assert(t, ok)

	var (
		creationTime     = time.Unix(0, data.CreationTime.Nanoseconds()).UTC()
		accessTime       = time.Unix(0, data.LastAccessTime.Nanoseconds()).UTC()
		modificationTime = time.Unix(0, data.LastWriteTime.Nanoseconds()).UTC()
	)

	assert.Equal(t, creationTime, mTime)
	assert.Equal(t, accessTime, aTime)
	assert.Equal(t, modificationTime, mTime)
}

// TestChtimesFollowsParentSymlink verifies that chtimes continues to work
// through symlinks in the parent path. Only the final path component is
// expected not to be a reparse point.
func TestChtimesFollowsParentSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	assert.NilError(t, os.Mkdir(filepath.Join(tmpDir, "dir"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(tmpDir, "dir", "file"), []byte("hello toto"), 0o644))
	assert.NilError(t, os.Symlink("dir", filepath.Join(tmpDir, "linked-dir")))

	root, err := os.OpenRoot(tmpDir)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	want := time.Date(2001, time.February, 3, 4, 5, 6, 0, time.UTC)
	assert.NilError(t, chtimes(root, filepath.Join("linked-dir", "file"), want, want))

	fi, err := root.Stat(filepath.Join("dir", "file"))
	assert.NilError(t, err)

	data, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	assert.Assert(t, ok)

	got := time.Unix(0, data.CreationTime.Nanoseconds()).UTC()
	assert.Equal(t, got, want)
}

// TestChtimesRejectsSymlink verifies that chtimes does not follow a final
// symlink. Tar symlink entries are handled separately through lchtimes, so a
// symlink encountered by chtimes indicates that the destination changed
// unexpectedly after its parent path was resolved.
func TestChtimesRejectsSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	target := filepath.Join(tmpDir, "target")
	assert.NilError(t, os.WriteFile(target, []byte("hello toto"), 0o644))

	link := filepath.Join(tmpDir, "link")
	assert.NilError(t, os.Symlink("target", link))

	root, err := os.OpenRoot(tmpDir)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	before, err := root.Stat("target")
	assert.NilError(t, err)
	beforeData, ok := before.Sys().(*syscall.Win32FileAttributeData)
	assert.Assert(t, ok)

	want := time.Date(2001, time.February, 3, 4, 5, 6, 0, time.UTC)
	err = chtimes(root, "link", want, want)

	// While it may not be a breakout (resolved location could be within rootFs),
	// we encountered a symlink for a path that we expected not to be.
	assert.Check(t, is.ErrorType(err, &breakoutErr{}))
	assert.Check(t, is.ErrorIs(err, windows.STATUS_REPARSE_POINT_ENCOUNTERED))

	// Verify that chtimes did not follow the symlink and modify its target.
	after, err := root.Stat("target")
	assert.NilError(t, err)
	afterData, ok := after.Sys().(*syscall.Win32FileAttributeData)
	assert.Assert(t, ok)

	assert.Equal(t, beforeData.CreationTime.Nanoseconds(), afterData.CreationTime.Nanoseconds())
	assert.Equal(t, beforeData.LastAccessTime.Nanoseconds(), afterData.LastAccessTime.Nanoseconds())
	assert.Equal(t, beforeData.LastWriteTime.Nanoseconds(), afterData.LastWriteTime.Nanoseconds())
}
