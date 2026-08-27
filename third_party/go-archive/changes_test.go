package archive

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"testing"
	"time"

	"github.com/moby/sys/user"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

func maxInt(x, y int) int {
	if x >= y {
		return x
	}
	return y
}

func copyDir(src, dst string) error {
	if runtime.GOOS != "windows" {
		return exec.Command("cp", "-a", src, dst).Run()
	}

	// Could have used xcopy src dst /E /I /H /Y /B. However, xcopy has the
	// unfortunate side effect of not preserving timestamps of newly created
	// directories in the target directory, so we don't get accurate changes.
	// Use robocopy instead. Note this isn't available in microsoft/nanoserver.
	// But it has gotchas. See https://weblogs.sqlteam.com/robv/archive/2010/02/17/61106.aspx
	err := exec.Command("robocopy", filepath.FromSlash(src), filepath.FromSlash(dst), "/SL", "/COPYALL", "/MIR").Run()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
			if status.ExitStatus()&24 == 0 {
				return nil
			}
		}
	}
	return err
}

type FileType uint32

const (
	Regular FileType = 0
	Dir     FileType = 1
	Symlink FileType = 2
)

type FileData struct {
	filetype    FileType
	path        string
	contents    string
	permissions os.FileMode
}

func createSampleDir(t *testing.T, root string) {
	files := []FileData{
		{filetype: Regular, path: "file1", contents: "file1\n", permissions: 0o600},
		{filetype: Regular, path: "file2", contents: "file2\n", permissions: 0o666},
		{filetype: Regular, path: "file3", contents: "file3\n", permissions: 0o404},
		{filetype: Regular, path: "file4", contents: "file4\n", permissions: 0o600},
		{filetype: Regular, path: "file5", contents: "file5\n", permissions: 0o600},
		{filetype: Regular, path: "file6", contents: "file6\n", permissions: 0o600},
		{filetype: Regular, path: "file7", contents: "file7\n", permissions: 0o600},
		{filetype: Dir, path: "dir1", contents: "", permissions: 0o740},
		// This entry was originally added as mode 0o1444 in
		// https://github.com/shykes/docker/commit/f7238f94e8b5b31e78360c342676ca84b5984806
		// https://github.com/shykes/docker/pull/30
		//
		// Go's os.FileMode does not encode the sticky bit as 0o1000. Historically,
		// os.WriteFile silently discarded that bit and created the file with mode
		// 0o444, whereas os.Root.WriteFile rejects the unsupported FileMode bit.
		// {filetype: Regular, path: "dir1/file1-1", contents: "file1-1\n", permissions: 0o1444},
		{filetype: Regular, path: "dir1/file1-1", contents: "file1-1\n", permissions: 0o444},
		{filetype: Regular, path: "dir1/file1-2", contents: "file1-2\n", permissions: 0o666},
		{filetype: Dir, path: "dir2", contents: "", permissions: 0o700},
		{filetype: Regular, path: "dir2/file2-1", contents: "file2-1\n", permissions: 0o666},
		{filetype: Regular, path: "dir2/file2-2", contents: "file2-2\n", permissions: 0o666},
		{filetype: Dir, path: "dir3", contents: "", permissions: 0o700},
		{filetype: Regular, path: "dir3/file3-1", contents: "file3-1\n", permissions: 0o666},
		{filetype: Regular, path: "dir3/file3-2", contents: "file3-2\n", permissions: 0o666},
		{filetype: Dir, path: "dir4", contents: "", permissions: 0o700},
		{filetype: Regular, path: "dir4/file3-1", contents: "file4-1\n", permissions: 0o666},
		{filetype: Regular, path: "dir4/file3-2", contents: "file4-2\n", permissions: 0o666},
		{filetype: Symlink, path: "symlink1", contents: "target1", permissions: 0o666},
		{filetype: Symlink, path: "symlink2", contents: "target2", permissions: 0o666},
		{filetype: Symlink, path: "symlink3", contents: filepath.Join(root, "file1"), permissions: 0o666},
		{filetype: Symlink, path: "symlink4", contents: filepath.Join(root, "symlink3"), permissions: 0o666},
		{filetype: Symlink, path: "dirSymlink", contents: filepath.Join(root, "dir1"), permissions: 0o740},
	}
	provisionSampleDir(t, root, files)
}

func provisionSampleDir(t *testing.T, rootPath string, files []FileData) {
	t.Helper()

	root, err := os.OpenRoot(rootPath)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	now := time.Now()
	for _, info := range files {
		name := filepath.FromSlash(info.path)
		switch info.filetype {
		case Dir:
			err := root.MkdirAll(name, info.permissions)
			assert.NilError(t, err)
		case Regular:
			err := root.WriteFile(name, []byte(info.contents), info.permissions)
			assert.NilError(t, err)
		case Symlink:
			err := root.Symlink(info.contents, name)
			assert.NilError(t, err)
		}

		if info.filetype != Symlink {
			// Set a consistent ctime, atime for all files and dirs
			err := chtimes(root, name, now, now)
			assert.NilError(t, err)
		}
	}
}

func TestChangeString(t *testing.T) {
	actual := (&Change{Path: "change", Kind: ChangeModify}).String()
	if actual != "C change" {
		t.Fatalf("String() of a change with ChangeModify Kind should have been %s but was %s", "C change", actual)
	}
	actual = (&Change{Path: "change", Kind: ChangeAdd}).String()
	if actual != "A change" {
		t.Fatalf("String() of a change with ChangeAdd Kind should have been %s but was %s", "A change", actual)
	}
	actual = (&Change{Path: "change", Kind: ChangeDelete}).String()
	if actual != "D change" {
		t.Fatalf("String() of a change with ChangeDelete Kind should have been %s but was %s", "D change", actual)
	}
}

func TestChangesWithNoChanges(t *testing.T) {
	rwLayer, err := os.MkdirTemp("", "docker-changes-test")
	assert.NilError(t, err)
	defer os.RemoveAll(rwLayer)
	layer, err := os.MkdirTemp("", "docker-changes-test-layer")
	assert.NilError(t, err)
	defer os.RemoveAll(layer)
	createSampleDir(t, layer)
	changes, err := Changes([]string{layer}, rwLayer)
	assert.NilError(t, err)
	if len(changes) != 0 {
		t.Fatalf("Changes with no difference should have detect no changes, but detected %d", len(changes))
	}
}

func TestChangesWithChanges(t *testing.T) {
	// Mock the readonly layer
	layer, err := os.MkdirTemp("", "docker-changes-test-layer")
	assert.NilError(t, err)
	defer os.RemoveAll(layer)
	createSampleDir(t, layer)
	assert.NilError(t, os.MkdirAll(filepath.Join(layer, "dir1", "subfolder"), 0o740))

	// Mock the RW layer
	rwLayer, err := os.MkdirTemp("", "docker-changes-test")
	assert.NilError(t, err)
	defer os.RemoveAll(rwLayer)

	// Create a folder in RW layer
	dir1 := filepath.Join(rwLayer, "dir1")
	assert.NilError(t, os.MkdirAll(dir1, 0o740))
	deletedFile := filepath.Join(dir1, ".wh.file1-2")
	assert.NilError(t, os.WriteFile(deletedFile, []byte{}, 0o600))
	modifiedFile := filepath.Join(dir1, "file1-1")
	assert.NilError(t, os.WriteFile(modifiedFile, []byte{0x00}, 0o1444))
	// Let's add a subfolder for a newFile
	subfolder := filepath.Join(dir1, "subfolder")
	assert.NilError(t, os.MkdirAll(subfolder, 0o740))
	newFile := filepath.Join(subfolder, "newFile")
	assert.NilError(t, os.WriteFile(newFile, []byte{}, 0o740))

	changes, err := Changes([]string{layer}, rwLayer)
	assert.NilError(t, err)

	expectedChanges := []Change{
		{Path: filepath.FromSlash("/dir1"), Kind: ChangeModify},
		{Path: filepath.FromSlash("/dir1/file1-1"), Kind: ChangeModify},
		{Path: filepath.FromSlash("/dir1/file1-2"), Kind: ChangeDelete},
		{Path: filepath.FromSlash("/dir1/subfolder"), Kind: ChangeModify},
		{Path: filepath.FromSlash("/dir1/subfolder/newFile"), Kind: ChangeAdd},
	}
	checkChanges(expectedChanges, changes, t)
}

// See https://github.com/docker/docker/pull/13590
func TestChangesWithChangesGH13590(t *testing.T) {
	// TODO Windows. Needs further investigation to identify the failure
	if runtime.GOOS == "windows" {
		t.Skip("needs more investigation")
	}
	baseLayer, err := os.MkdirTemp("", "docker-changes-test.")
	assert.NilError(t, err)
	defer os.RemoveAll(baseLayer)

	dir3 := filepath.Join(baseLayer, "dir1", "dir2", "dir3")
	assert.NilError(t, os.MkdirAll(dir3, 0o740))

	file := filepath.Join(dir3, "file.txt")
	assert.NilError(t, os.WriteFile(file, []byte("hello"), 0o666))

	layer, err := os.MkdirTemp("", "docker-changes-test2.")
	assert.NilError(t, err)
	defer os.RemoveAll(layer)

	// Test creating a new file
	if err := copyDir(filepath.Join(baseLayer, "dir1"), layer); err != nil {
		t.Fatalf("Cmd failed: %q", err)
	}

	assert.NilError(t, os.Remove(filepath.Join(layer, "dir1", "dir2", "dir3", "file.txt")))
	file = filepath.Join(layer, "dir1", "dir2", "dir3", "file1.txt")
	assert.NilError(t, os.WriteFile(file, []byte("bye"), 0o666))

	changes, err := Changes([]string{baseLayer}, layer)
	assert.NilError(t, err)

	expectedChanges := []Change{
		{Path: filepath.FromSlash("/dir1/dir2/dir3"), Kind: ChangeModify},
		{Path: filepath.FromSlash("/dir1/dir2/dir3/file1.txt"), Kind: ChangeAdd},
	}
	checkChanges(expectedChanges, changes, t)

	// Now test changing a file
	layer, err = os.MkdirTemp("", "docker-changes-test3.")
	assert.NilError(t, err)
	defer os.RemoveAll(layer)

	if err := copyDir(filepath.Join(baseLayer, "dir1"), layer); err != nil {
		t.Fatalf("Cmd failed: %q", err)
	}

	file = filepath.Join(layer, "dir1", "dir2", "dir3", "file.txt")
	assert.NilError(t, os.WriteFile(file, []byte("bye"), 0o666))

	changes, err = Changes([]string{baseLayer}, layer)
	assert.NilError(t, err)

	expectedChanges = []Change{
		{Path: filepath.FromSlash("/dir1/dir2/dir3/file.txt"), Kind: ChangeModify},
	}
	checkChanges(expectedChanges, changes, t)
}

// Create a directory, copy it, make sure we report no changes between the two
func TestChangesDirsEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIXME: broken on Windows 1903 and up; see https://github.com/moby/moby/pull/39846")
	}

	src, err := os.MkdirTemp("", "docker-changes-test")
	assert.NilError(t, err)
	defer os.RemoveAll(src)
	createSampleDir(t, src)
	dst := src + "-copy"
	err = copyDir(src, dst)
	assert.NilError(t, err)
	defer os.RemoveAll(dst)
	changes, err := ChangesDirs(dst, src)
	assert.NilError(t, err)

	if len(changes) != 0 {
		t.Fatalf("Reported changes for identical dirs: %v", changes)
	}
	assert.NilError(t, os.RemoveAll(src))
	assert.NilError(t, os.RemoveAll(dst))
}

func mutateSampleDir(t *testing.T, rootPath string) {
	t.Helper()

	root, err := os.OpenRoot(rootPath)
	assert.NilError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	// Remove a regular file
	err = root.RemoveAll("file1")
	assert.NilError(t, err)

	// Remove a directory
	err = root.RemoveAll("dir1")
	assert.NilError(t, err)

	// Remove a symlink
	err = root.RemoveAll("symlink1")
	assert.NilError(t, err)

	// Rewrite a file
	err = root.WriteFile("file2", []byte("fileNN\n"), 0o777)
	assert.NilError(t, err)

	// Replace a file
	err = root.RemoveAll("file3")
	assert.NilError(t, err)
	err = root.WriteFile("file3", []byte("fileMM\n"), 0o404)
	assert.NilError(t, err)

	// Touch file
	err = chtimes(root, "file4", time.Now().Add(time.Second), time.Now().Add(time.Second))
	assert.NilError(t, err)

	// Replace file with dir
	err = root.RemoveAll("file5")
	assert.NilError(t, err)
	err = root.Mkdir("file5", 0o666)
	assert.NilError(t, err)

	// Create new file
	err = root.WriteFile("filenew", []byte("filenew\n"), 0o777)
	assert.NilError(t, err)

	// Create new dir
	err = root.MkdirAll("dirnew", 0o766)
	assert.NilError(t, err)

	// Create a new symlink
	err = root.Symlink("targetnew", "symlinknew")
	assert.NilError(t, err)

	// Change a symlink
	err = root.RemoveAll("symlink2")
	assert.NilError(t, err)

	err = root.Symlink("target2change", "symlink2")
	assert.NilError(t, err)

	// Replace dir with file
	err = root.RemoveAll("dir2")
	assert.NilError(t, err)
	err = root.WriteFile("dir2", []byte("dir2\n"), 0o777)
	assert.NilError(t, err)

	// Touch dir
	err = chtimes(root, "dir3", time.Now().Add(time.Second), time.Now().Add(time.Second))
	assert.NilError(t, err)
}

func TestChangesDirsMutated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIXME: broken on Windows 1903 and up; see https://github.com/moby/moby/pull/39846")
	}

	src, err := os.MkdirTemp("", "docker-changes-test")
	assert.NilError(t, err)
	createSampleDir(t, src)
	dst := src + "-copy"
	err = copyDir(src, dst)
	assert.NilError(t, err)
	defer func() {
		_ = os.RemoveAll(dst)
		_ = os.RemoveAll(src)
	}()

	mutateSampleDir(t, dst)

	changes, err := ChangesDirs(dst, src)
	assert.NilError(t, err)

	sort.Sort(changesByPath(changes))

	expectedChanges := []Change{
		{Path: filepath.FromSlash("/dir1"), Kind: ChangeDelete},
		{Path: filepath.FromSlash("/dir2"), Kind: ChangeModify},
	}

	// Note there is slight difference between the Linux and Windows
	// implementations here. Due to https://github.com/moby/moby/issues/9874,
	// and the fix at https://github.com/moby/moby/pull/11422, Linux does not
	// consider a change to the directory time as a change. Windows on NTFS
	// does. See https://github.com/moby/moby/pull/37982 for more information.
	//
	// Note also: https://github.com/moby/moby/pull/37982#discussion_r223523114
	// that differences are ordered in the way the test is currently written, hence
	// this is in the middle of the list of changes rather than at the start or
	// end. Potentially can be addressed later.
	if runtime.GOOS == "windows" {
		expectedChanges = append(expectedChanges, Change{Path: filepath.FromSlash("/dir3"), Kind: ChangeModify})
	}

	expectedChanges = append(expectedChanges, []Change{
		{Path: filepath.FromSlash("/dirnew"), Kind: ChangeAdd},
		{Path: filepath.FromSlash("/file1"), Kind: ChangeDelete},
		{Path: filepath.FromSlash("/file2"), Kind: ChangeModify},
		{Path: filepath.FromSlash("/file3"), Kind: ChangeModify},
		{Path: filepath.FromSlash("/file4"), Kind: ChangeModify},
		{Path: filepath.FromSlash("/file5"), Kind: ChangeModify},
		{Path: filepath.FromSlash("/filenew"), Kind: ChangeAdd},
		{Path: filepath.FromSlash("/symlink1"), Kind: ChangeDelete},
		{Path: filepath.FromSlash("/symlink2"), Kind: ChangeModify},
		{Path: filepath.FromSlash("/symlinknew"), Kind: ChangeAdd},
	}...)

	for i := 0; i < maxInt(len(changes), len(expectedChanges)); i++ {
		if i >= len(expectedChanges) {
			t.Fatalf("unexpected change %s\n", changes[i].String())
		}
		if i >= len(changes) {
			t.Fatalf("no change for expected change %s\n", expectedChanges[i].String())
		}
		if changes[i].Path == expectedChanges[i].Path {
			if changes[i] != expectedChanges[i] {
				t.Fatalf("Wrong change for %s, expected %s, got %s\n", changes[i].Path, changes[i].String(), expectedChanges[i].String())
			}
		} else if changes[i].Path < expectedChanges[i].Path {
			t.Fatalf("unexpected change %q %q\n", changes[i].String(), expectedChanges[i].Path)
		} else {
			t.Fatalf("no change for expected change %s != %s\n", expectedChanges[i].String(), changes[i].String())
		}
	}
}

func TestApplyLayer(t *testing.T) {
	// TODO Windows. This is very close to working, but it fails with changes
	// to \symlinknew and \symlink2. The destination has an updated
	// Access/Modify/Change/Birth date to the source (~3/100th sec different).
	// Needs further investigation as to why, but I currently believe this is
	// just the way NTFS works. I don't think it's a bug in this test or archive.
	if runtime.GOOS == "windows" {
		t.Skip("needs further investigation")
	}
	// TODO macOS: Test is failing: changes_test.go:440: Unexpected differences after reapplying mutation: [{/symlinknew C} {/symlink2 C}]
	if runtime.GOOS == "darwin" {
		t.Skip("needs further investigation")
	}
	src, err := os.MkdirTemp("", "docker-changes-test")
	assert.NilError(t, err)
	createSampleDir(t, src)
	defer os.RemoveAll(src)
	dst := src + "-copy"
	err = copyDir(src, dst)
	assert.NilError(t, err)
	mutateSampleDir(t, dst)
	defer os.RemoveAll(dst)

	changes, err := ChangesDirs(dst, src)
	assert.NilError(t, err)

	layer, err := ExportChanges(dst, changes, user.IdentityMapping{})
	assert.NilError(t, err)

	layerCopy, err := newTempArchive(layer, "")
	assert.NilError(t, err)

	_, err = ApplyLayer(src, layerCopy)
	assert.NilError(t, err)

	changes2, err := ChangesDirs(src, dst)
	assert.NilError(t, err)

	if len(changes2) != 0 {
		t.Fatalf("Unexpected differences after reapplying mutation: %v", changes2)
	}
}

func TestChangesSizeWithHardlinks(t *testing.T) {
	// TODO Windows. Needs further investigation. Likely in ChangeSizes not
	// coping correctly with hardlinks on Windows.
	if runtime.GOOS == "windows" {
		t.Skip("needs further investigation")
	}
	srcDir, err := os.MkdirTemp("", "docker-test-srcDir")
	assert.NilError(t, err)
	defer os.RemoveAll(srcDir)

	destDir, err := os.MkdirTemp("", "docker-test-destDir")
	assert.NilError(t, err)
	defer os.RemoveAll(destDir)

	creationSize, err := prepareUntarSourceDirectory(100, destDir, true)
	assert.NilError(t, err)

	changes, err := ChangesDirs(destDir, srcDir)
	assert.NilError(t, err)

	got := ChangesSize(destDir, changes)
	if got != int64(creationSize) {
		t.Errorf("Expected %d bytes of changes, got %d", creationSize, got)
	}
}

func TestChangesSizeWithNoChanges(t *testing.T) {
	size := ChangesSize("/tmp", nil)
	if size != 0 {
		t.Fatalf("ChangesSizes with no changes should be 0, was %d", size)
	}
}

func TestChangesSizeWithOnlyDeleteChanges(t *testing.T) {
	size := ChangesSize("/tmp", []Change{
		{Path: "deletedPath", Kind: ChangeDelete},
	})
	if size != 0 {
		t.Fatalf("ChangesSizes with only delete changes should be 0, was %d", size)
	}
}

func TestChangesSize(t *testing.T) {
	parentPath, err := os.MkdirTemp("", "docker-changes-test")
	assert.NilError(t, err)
	defer os.RemoveAll(parentPath)
	addition := filepath.Join(parentPath, "addition")
	err = os.WriteFile(addition, []byte{0x01, 0x01, 0x01}, 0o744)
	assert.NilError(t, err)
	modification := filepath.Join(parentPath, "modification")
	err = os.WriteFile(modification, []byte{0x01, 0x01, 0x01}, 0o744)
	assert.NilError(t, err)

	size := ChangesSize(parentPath, []Change{
		{Path: "addition", Kind: ChangeAdd},
		{Path: "modification", Kind: ChangeModify},
	})
	if size != 6 {
		t.Fatalf("Expected 6 bytes of changes, got %d", size)
	}
}

func checkChanges(expectedChanges, changes []Change, t *testing.T) {
	skip.If(t, runtime.GOOS != "windows" && os.Getuid() != 0, "skipping test that requires root")
	sort.Sort(changesByPath(expectedChanges))
	sort.Sort(changesByPath(changes))
	for i := 0; i < maxInt(len(changes), len(expectedChanges)); i++ {
		if i >= len(expectedChanges) {
			t.Fatalf("unexpected change %s\n", changes[i].String())
		}
		if i >= len(changes) {
			t.Fatalf("no change for expected change %s\n", expectedChanges[i].String())
		}
		if changes[i].Path == expectedChanges[i].Path {
			if changes[i] != expectedChanges[i] {
				t.Fatalf("Wrong change for %s, expected %s, got %s\n", changes[i].Path, changes[i].String(), expectedChanges[i].String())
			}
		} else if changes[i].Path < expectedChanges[i].Path {
			t.Fatalf("unexpected change %s\n", changes[i].String())
		} else {
			t.Fatalf("no change for expected change %s != %s\n", expectedChanges[i].String(), changes[i].String())
		}
	}
}
