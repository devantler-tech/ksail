package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/moby/sys/user"
	"github.com/moby/sys/userns"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/skip"

	"github.com/moby/go-archive/compression"
)

var defaultArchiver = NewDefaultArchiver()

func defaultTarUntar(src, dst string) error {
	return defaultArchiver.TarUntar(src, dst)
}

func defaultUntarPath(src, dst string) error {
	return defaultArchiver.UntarPath(src, dst)
}

func defaultCopyFileWithTar(src, dst string) (err error) {
	return defaultArchiver.CopyFileWithTar(src, dst)
}

func defaultCopyWithTar(src, dst string) error {
	return defaultArchiver.CopyWithTar(src, dst)
}

func createTarFromFiles(t *testing.T, tarPath string, files ...string) {
	t.Helper()

	f, err := os.Create(tarPath)
	assert.NilError(t, err)

	tw := tar.NewWriter(f)
	for _, filePath := range files {
		info, err := os.Stat(filePath)
		assert.NilError(t, err)

		hdr, err := tar.FileInfoHeader(info, "")
		assert.NilError(t, err)

		// None of the tests require nested paths, so we simplify
		// creation using just the basename to avoid conversion to/from
		// POSIX paths (as used in Tar headers).
		hdr.Name = filepath.Base(filePath)
		assert.NilError(t, tw.WriteHeader(hdr))

		if !info.Mode().IsRegular() {
			continue
		}

		src, err := os.Open(filePath)
		assert.NilError(t, err)

		_, copyErr := io.Copy(tw, src)
		closeErr := src.Close()
		assert.NilError(t, copyErr)
		assert.NilError(t, closeErr)
	}

	assert.NilError(t, tw.Close())
	assert.NilError(t, f.Close())
}

func TestBreakoutErrorUnwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := breakoutError(sentinel)

	assert.Check(t, is.ErrorType(err, &breakoutErr{}))
	assert.Check(t, is.ErrorIs(err, sentinel))
}

func TestIsArchivePathDir(t *testing.T) {
	tmp := t.TempDir()
	assert.NilError(t, os.Mkdir(filepath.Join(tmp, "archivedir"), 0o755))
	assert.Check(t, !IsArchivePath(filepath.Join(tmp, "archivedir")), "incorrectly recognised directory as an archive")
}

func TestIsArchivePathInvalidFile(t *testing.T) {
	tmp := t.TempDir()

	archive := filepath.Join(tmp, "archive")
	assert.NilError(t, os.WriteFile(archive, []byte("hello"), 0o644))

	archiveGz := archive + ".gz"
	f, err := os.Create(archiveGz)
	assert.NilError(t, err)

	gw := gzip.NewWriter(f)
	_, err = gw.Write([]byte("hello"))
	assert.NilError(t, err)
	assert.NilError(t, gw.Close())
	assert.NilError(t, f.Close())

	assert.Check(t, !IsArchivePath(archive), "incorrectly recognised invalid tar path as archive")
	assert.Check(t, !IsArchivePath(archiveGz), "incorrectly recognised invalid compressed tar path as archive")
}

func TestIsArchivePathTar(t *testing.T) {
	tmp := t.TempDir()
	srcFile := filepath.Join(tmp, "archivedata")
	f, err := os.Create(srcFile)
	assert.NilError(t, err)
	assert.NilError(t, f.Close())

	tarFile := filepath.Join(tmp, "archive")
	createTarFromFiles(t, tarFile, srcFile)

	gzFile := tarFile + ".gz"
	in, err := os.Open(tarFile)
	assert.NilError(t, err)
	defer in.Close()

	out, err := os.Create(gzFile)
	assert.NilError(t, err)

	gw := gzip.NewWriter(out)

	_, err = io.Copy(gw, in)
	assert.NilError(t, err)
	assert.NilError(t, gw.Close())
	assert.NilError(t, out.Close())

	assert.Check(t, IsArchivePath(tarFile), "did not recognise valid tar path as archive")
	assert.Check(t, IsArchivePath(gzFile), "did not recognise valid compressed tar path as archive")
}

func TestUntarPathWithInvalidDest(t *testing.T) {
	tempFolder := t.TempDir()

	tarFile := filepath.Join(tempFolder, "src.tar")
	f, err := os.Create(tarFile)
	assert.NilError(t, err)

	tw := tar.NewWriter(f)
	assert.NilError(t, tw.WriteHeader(&tar.Header{
		Name: "src",
		Mode: 0o644,
		Size: 0,
	}))
	assert.NilError(t, tw.Close())
	assert.NilError(t, f.Close())

	invalidDest := filepath.Join(tempFolder, "invalidDest")
	d, err := os.Create(invalidDest) // being a file (not dir) should cause an error
	assert.NilError(t, err)
	assert.NilError(t, d.Close())

	err = defaultUntarPath(tarFile, invalidDest)
	assert.Assert(t, err != nil, "UntarPath with invalid destination path should return an error")
}

func TestUntarPathWithInvalidSrc(t *testing.T) {
	err := defaultUntarPath("/invalid/path", t.TempDir())
	if err == nil {
		t.Fatalf("UntarPath with invalid src path should throw an error.")
	}
}

func TestUntarPath(t *testing.T) {
	skip.If(t, runtime.GOOS != "windows" && os.Getuid() != 0, "skipping test that requires root")
	tmpFolder := t.TempDir()
	srcFile := filepath.Join(tmpFolder, "src")
	src, err := os.Create(srcFile)
	assert.NilError(t, err)
	assert.NilError(t, src.Close())

	tarFile := filepath.Join(tmpFolder, "src.tar")
	createTarFromFiles(t, tarFile, srcFile)

	destFolder := filepath.Join(tmpFolder, "dest")
	assert.NilError(t, os.MkdirAll(destFolder, 0o740))

	err = defaultUntarPath(tarFile, destFolder)
	assert.NilError(t, err, "UntarPath shouldn't return an error")

	expectedFile := filepath.Join(destFolder, filepath.Base(srcFile))
	_, err = os.Stat(expectedFile)
	assert.NilError(t, err, "destination folder should contain the source file")
}

// Do the same test as above but with the destination as file, it should fail
func TestUntarPathWithDestinationFile(t *testing.T) {
	tmpFolder := t.TempDir()
	srcFile := filepath.Join(tmpFolder, "src")
	src, err := os.Create(srcFile)
	assert.NilError(t, err)
	assert.NilError(t, src.Close())

	tarFile := filepath.Join(tmpFolder, "src.tar")
	createTarFromFiles(t, tarFile, srcFile)

	destFile := filepath.Join(tmpFolder, "dest")
	f, err := os.Create(destFile)
	assert.NilError(t, err)
	assert.NilError(t, f.Close())

	err = defaultUntarPath(tarFile, destFile)
	assert.Assert(t, err != nil, "UntarPath should return an error if the destination is a file")
}

// Do the same test as above but with the destination folder already exists
// and the destination file is a directory
// It's working, see https://github.com/docker/docker/issues/10040
func TestUntarPathWithDestinationSrcFileAsFolder(t *testing.T) {
	tmpFolder := t.TempDir()
	srcFile := filepath.Join(tmpFolder, "src")
	src, err := os.Create(srcFile)
	assert.NilError(t, err)
	assert.NilError(t, src.Close())

	tarFile := filepath.Join(tmpFolder, "src.tar")
	createTarFromFiles(t, tarFile, srcFile)

	destFolder := filepath.Join(tmpFolder, "dest")
	assert.NilError(t, os.MkdirAll(destFolder, 0o740))

	// Let's create a folder that will has the same path as the extracted file (from tar)
	destSrcFileAsFolder := filepath.Join(destFolder, filepath.Base(srcFile))
	assert.NilError(t, os.MkdirAll(destSrcFileAsFolder, 0o740))

	err = defaultUntarPath(tarFile, destFolder)
	assert.NilError(t, err, "UntarPath should not return an error if the extracted file already exists as a directory")
}

func TestCopyWithTarInvalidSrc(t *testing.T) {
	tempFolder := t.TempDir()
	destFolder := filepath.Join(tempFolder, "dest")
	invalidSrc := filepath.Join(tempFolder, "doesnotexists")
	err := os.MkdirAll(destFolder, 0o740)
	if err != nil {
		t.Fatal(err)
	}
	err = defaultCopyWithTar(invalidSrc, destFolder)
	if err == nil {
		t.Fatalf("archiver.CopyWithTar with invalid src path should throw an error.")
	}
}

func TestCopyWithTarInexistentDestWillCreateIt(t *testing.T) {
	skip.If(t, runtime.GOOS != "windows" && os.Getuid() != 0, "skipping test that requires root")
	tempFolder := t.TempDir()
	srcFolder := filepath.Join(tempFolder, "src")
	inexistentDestFolder := filepath.Join(tempFolder, "doesnotexists")
	err := os.MkdirAll(srcFolder, 0o740)
	if err != nil {
		t.Fatal(err)
	}
	err = defaultCopyWithTar(srcFolder, inexistentDestFolder)
	if err != nil {
		t.Fatalf("CopyWithTar with an inexistent folder shouldn't fail.")
	}
	_, err = os.Stat(inexistentDestFolder)
	if err != nil {
		t.Fatalf("CopyWithTar with an inexistent folder should create it.")
	}
}

// Test CopyWithTar with a file as src
func TestCopyWithTarSrcFile(t *testing.T) {
	folder := t.TempDir()
	dest := filepath.Join(folder, "dest")
	srcFolder := filepath.Join(folder, "src")
	src := filepath.Join(folder, filepath.Join("src", "src"))
	if err := os.MkdirAll(srcFolder, 0o740); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0o740); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("content"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := defaultCopyWithTar(src, dest); err != nil {
		t.Fatalf("archiver.CopyWithTar shouldn't throw an error, %s.", err)
	}
	// FIXME Check the content
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("Destination file should be the same as the source.")
	}
}

// Test CopyWithTar with a folder as src
func TestCopyWithTarSrcFolder(t *testing.T) {
	folder := t.TempDir()
	dest := filepath.Join(folder, "dest")
	src := filepath.Join(folder, filepath.Join("src", "folder"))
	if err := os.MkdirAll(src, 0o740); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0o740); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file"), []byte("content"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := defaultCopyWithTar(src, dest); err != nil {
		t.Fatalf("archiver.CopyWithTar shouldn't throw an error, %s.", err)
	}
	// FIXME Check the content (the file inside)
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("Destination folder should contain the source file but did not.")
	}
}

func TestCopyFileWithTarInvalidSrc(t *testing.T) {
	tempFolder := t.TempDir()
	destFolder := filepath.Join(tempFolder, "dest")
	err := os.MkdirAll(destFolder, 0o740)
	if err != nil {
		t.Fatal(err)
	}
	invalidFile := filepath.Join(tempFolder, "doesnotexists")
	err = defaultCopyFileWithTar(invalidFile, destFolder)
	if err == nil {
		t.Fatalf("archiver.CopyWithTar with invalid src path should throw an error.")
	}
}

func TestCopyFileWithTarInexistentDestWillCreateIt(t *testing.T) {
	tempFolder := t.TempDir()
	srcFile := filepath.Join(tempFolder, "src")
	inexistentDestFolder := filepath.Join(tempFolder, "doesnotexists")
	f, err := os.Create(srcFile)
	if assert.Check(t, err) {
		_ = f.Close()
	}
	err = defaultCopyFileWithTar(srcFile, inexistentDestFolder)
	if err != nil {
		t.Fatalf("CopyWithTar with an inexistent folder shouldn't fail.")
	}
	_, err = os.Stat(inexistentDestFolder)
	if err != nil {
		t.Fatalf("CopyWithTar with an inexistent folder should create it.")
	}
	// FIXME Test the src file and content
}

func TestCopyFileWithTarSrcFolder(t *testing.T) {
	folder := t.TempDir()
	dest := filepath.Join(folder, "dest")
	src := filepath.Join(folder, "srcfolder")
	err := os.MkdirAll(src, 0o740)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(dest, 0o740)
	if err != nil {
		t.Fatal(err)
	}
	err = defaultCopyFileWithTar(src, dest)
	if err == nil {
		t.Fatalf("CopyFileWithTar should throw an error with a folder.")
	}
}

func TestCopyFileWithTarSrcFile(t *testing.T) {
	folder := t.TempDir()
	dest := filepath.Join(folder, "dest")
	srcFolder := filepath.Join(folder, "src")
	src := filepath.Join(folder, filepath.Join("src", "src"))
	if err := os.MkdirAll(srcFolder, 0o740); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0o740); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("content"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := defaultCopyWithTar(src, dest+"/"); err != nil {
		t.Fatalf("archiver.CopyFileWithTar shouldn't throw an error, %s.", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("Destination folder should contain the source file but did not.")
	}
}

func TestTarFiles(t *testing.T) {
	// try without hardlinks
	if err := checkNoChanges(t, 1000, false); err != nil {
		t.Fatal(err)
	}
	// try with hardlinks
	if err := checkNoChanges(t, 1000, true); err != nil {
		t.Fatal(err)
	}
}

func checkNoChanges(t *testing.T, fileNum int, hardlinks bool) error {
	srcDir, err := os.MkdirTemp(t.TempDir(), "srcDir")
	if err != nil {
		return err
	}

	destDir, err := os.MkdirTemp(t.TempDir(), "destDir")
	if err != nil {
		return err
	}

	_, err = prepareUntarSourceDirectory(fileNum, srcDir, hardlinks)
	if err != nil {
		return err
	}

	err = defaultTarUntar(srcDir, destDir)
	if err != nil {
		return err
	}

	changes, err := ChangesDirs(destDir, srcDir)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		return fmt.Errorf("with %d files and %v hardlinks: expected 0 changes, got %d", fileNum, hardlinks, len(changes))
	}
	return nil
}

func tarUntar(t *testing.T, origin string, options *TarOptions) ([]Change, error) {
	archive, err := TarWithOptions(origin, options)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	buf := make([]byte, 10)
	if _, err := io.ReadFull(archive, buf); err != nil {
		return nil, err
	}
	wrap := io.MultiReader(bytes.NewReader(buf), archive)

	detectedCompression := compression.Detect(buf)
	expected := options.Compression
	if detectedCompression.Extension() != expected.Extension() {
		return nil, fmt.Errorf("wrong compression detected; expected: %s, got: %s", expected.Extension(), detectedCompression.Extension())
	}

	tmp := t.TempDir()
	if err := Untar(wrap, tmp, nil); err != nil {
		return nil, err
	}
	if _, err := os.Stat(tmp); err != nil {
		return nil, err
	}

	return ChangesDirs(origin, tmp)
}

func TestTarUntar(t *testing.T) {
	origin := t.TempDir()
	if err := os.WriteFile(filepath.Join(origin, "1"), []byte("hello world"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, "2"), []byte("welcome!"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, "3"), []byte("will be ignored"), 0o700); err != nil {
		t.Fatal(err)
	}

	for _, c := range []compression.Compression{
		compression.None,
		compression.Gzip,
	} {
		changes, err := tarUntar(t, origin, &TarOptions{
			Compression:     c,
			ExcludePatterns: []string{"3"},
		})
		if err != nil {
			t.Fatalf("Error tar/untar for compression %s: %s", c.Extension(), err)
		}

		if len(changes) != 1 || changes[0].Path != string(filepath.Separator)+"3" {
			t.Fatalf("Unexpected differences after tarUntar: %v", changes)
		}
	}
}

func TestTarWithOptionsChownOptsAlwaysOverridesIdPair(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "1")
	err := os.WriteFile(filePath, []byte("hello world"), 0o700)
	assert.NilError(t, err)

	idMaps := []user.IDMap{
		0: {
			ID:       0,
			ParentID: 0,
			Count:    65536,
		},
		1: {
			ID:       0,
			ParentID: 100000,
			Count:    65536,
		},
	}

	tests := []struct {
		opts        *TarOptions
		expectedUID int
		expectedGID int
	}{
		{&TarOptions{ChownOpts: &ChownOpts{UID: 1337, GID: 42}}, 1337, 42},
		{&TarOptions{ChownOpts: &ChownOpts{UID: 100001, GID: 100001}, IDMap: user.IdentityMapping{UIDMaps: idMaps, GIDMaps: idMaps}}, 100001, 100001},
		{&TarOptions{ChownOpts: &ChownOpts{UID: 0, GID: 0}, NoLchown: false}, 0, 0},
		{&TarOptions{ChownOpts: &ChownOpts{UID: 1, GID: 1}, NoLchown: true}, 1, 1},
		{&TarOptions{ChownOpts: &ChownOpts{UID: 1000, GID: 1000}, NoLchown: true}, 1000, 1000},
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			reader, err := TarWithOptions(filePath, tc.opts)
			assert.NilError(t, err)
			tr := tar.NewReader(reader)
			defer reader.Close()
			for {
				hdr, err := tr.Next()
				if errors.Is(err, io.EOF) {
					// end of tar archive
					break
				}
				assert.NilError(t, err)
				assert.Check(t, is.Equal(hdr.Uid, tc.expectedUID), "Uid equals expected value")
				assert.Check(t, is.Equal(hdr.Gid, tc.expectedGID), "Gid equals expected value")
			}
		})
	}
}

func TestTarWithOptions(t *testing.T) {
	origin := t.TempDir()
	if _, err := os.MkdirTemp(origin, "folder"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, "1"), []byte("hello world"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, "2"), []byte("welcome!"), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		opts       *TarOptions
		numChanges int
	}{
		{&TarOptions{IncludeFiles: []string{"1"}}, 2},
		{&TarOptions{ExcludePatterns: []string{"2"}}, 1},
		{&TarOptions{ExcludePatterns: []string{"1", "folder*"}}, 2},
		{&TarOptions{IncludeFiles: []string{"1", "1"}}, 2},
		{&TarOptions{IncludeFiles: []string{"1"}, RebaseNames: map[string]string{"1": "test"}}, 4},
	}
	for _, tc := range tests {
		changes, err := tarUntar(t, origin, tc.opts)
		if err != nil {
			t.Fatalf("Error tar/untar when testing inclusion/exclusion: %s", err)
		}
		if len(changes) != tc.numChanges {
			t.Errorf("Expected %d changes, got %d for %+v:",
				tc.numChanges, len(changes), tc.opts)
		}
	}
}

// Some tar archives such as http://haproxy.1wt.eu/download/1.5/src/devel/haproxy-1.5-dev21.tar.gz
// use PAX Global Extended Headers.
// Failing prevents the archives from being uncompressed during ADD
func TestTypeXGlobalHeaderDoesNotFail(t *testing.T) {
	hdr := tar.Header{Typeflag: tar.TypeXGlobalHeader}
	tmpDir := t.TempDir()
	root, err := os.OpenRoot(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	err = createTarFile(root, "pax_global_header", &hdr, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}

// TestCreateTarFileSymlinkPreservesLinkname verifies that symlink targets are
// treated as opaque values and are preserved verbatim rather than converted to
// platform-native path syntax during extraction.
func TestCreateTarFileSymlinkPreservesLinkname(t *testing.T) {
	tests := []struct {
		name     string
		linkname string
	}{
		{
			name:     "relative_posix_target",
			linkname: "../usr/local/bin/tool",
		},
		{
			name:     "absolute_posix_target",
			linkname: "/usr/local/bin/tool",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			root, err := os.OpenRoot(tmpDir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()

			if err := root.Mkdir("bin", 0o755); err != nil {
				t.Fatal(err)
			}

			hdr := tar.Header{
				Name:     "bin/tool",
				Typeflag: tar.TypeSymlink,
				Linkname: tc.linkname,
			}

			err = createTarFile(root, hdr.Name, &hdr, nil, &TarOptions{
				NoLchown: true,
			})
			if err != nil {
				t.Fatal(err)
			}

			target, err := os.Readlink(filepath.Join(tmpDir, "bin", "tool"))
			if err != nil {
				t.Fatal(err)
			}
			assert.Check(t, is.Equal(tc.linkname, target))
		})
	}
}

// Some tar have both GNU specific (huge uid) and Ustar specific (long name) things.
// Not supposed to happen (should use PAX instead of Ustar for long name) but it does and it should still work.
func TestUntarUstarGnuConflict(t *testing.T) {
	f, err := os.Open("testdata/broken.tar")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	found := false
	tr := tar.NewReader(f)
	// Iterate through the files in the archive.
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			// end of tar archive
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "root/.cpanm/work/1395823785.24209/Plack-1.0030/blib/man3/Plack::Middleware::LighttpdScriptNameFix.3pm" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s not found in the archive", "root/.cpanm/work/1395823785.24209/Plack-1.0030/blib/man3/Plack::Middleware::LighttpdScriptNameFix.3pm")
	}
}

func prepareUntarSourceDirectory(numberOfFiles int, targetPath string, makeLinks bool) (int, error) {
	fileData := []byte("fooo")
	for n := range numberOfFiles {
		fileName := fmt.Sprintf("file-%d", n)
		if err := os.WriteFile(filepath.Join(targetPath, fileName), fileData, 0o700); err != nil {
			return 0, err
		}
		if makeLinks {
			if err := os.Link(filepath.Join(targetPath, fileName), filepath.Join(targetPath, fileName+"-link")); err != nil {
				return 0, err
			}
		}
	}
	totalSize := numberOfFiles * len(fileData)
	return totalSize, nil
}

func BenchmarkTarUntar(b *testing.B) {
	origin, err := os.MkdirTemp(b.TempDir(), "docker-test-untar-origin")
	if err != nil {
		b.Fatal(err)
	}
	tempDir, err := os.MkdirTemp(b.TempDir(), "docker-test-untar-destination")
	if err != nil {
		b.Fatal(err)
	}
	target := filepath.Join(tempDir, "dest")
	n, err := prepareUntarSourceDirectory(100, origin, false)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.SetBytes(int64(n))
	for n := 0; n < b.N; n++ {
		err := defaultTarUntar(origin, target)
		if err != nil {
			b.Fatal(err)
		}
		err = os.RemoveAll(target)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTarUntarWithLinks(b *testing.B) {
	origin, err := os.MkdirTemp(b.TempDir(), "docker-test-untar-origin")
	if err != nil {
		b.Fatal(err)
	}
	tempDir, err := os.MkdirTemp(b.TempDir(), "docker-test-untar-destination")
	if err != nil {
		b.Fatal(err)
	}
	target := filepath.Join(tempDir, "dest")
	n, err := prepareUntarSourceDirectory(100, origin, true)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.SetBytes(int64(n))
	for n := 0; n < b.N; n++ {
		err := defaultTarUntar(origin, target)
		if err != nil {
			b.Fatal(err)
		}
		err = os.RemoveAll(target)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestUntarInvalidFilenames(t *testing.T) {
	for i, headers := range [][]*tar.Header{
		{
			{
				Name:     "../victim/dotdot",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
			},
		},
		{
			{
				// Note the leading slash
				Name:     "/../victim/slash-dotdot",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
			},
		},
	} {
		if err := testBreakout("untar", "docker-TestUntarInvalidFilenames", headers); err != nil {
			t.Fatalf("i=%d. %v", i, err)
		}
	}
}

// TestUntarParentTraversalContained verifies that entries whose names traverse
// above the destination (including a bare "..") are rejected and never write
// into the destination's parent. Regression test for the "write to the parent
// of the extraction root" breakout.
func TestUntarParentTraversalContained(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
	}{
		{name: "bare parent", entry: ".."},
		{name: "parent child", entry: "../pwned"},
		{name: "nested parent", entry: "../../pwned"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			dest := filepath.Join(base, "dest")
			assert.NilError(t, os.Mkdir(dest, 0o755))

			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			assert.NilError(t, tw.WriteHeader(&tar.Header{
				Name:     tc.entry,
				Typeflag: tar.TypeReg,
				Mode:     0o644,
				Size:     int64(len("bad")),
			}))
			_, err := tw.Write([]byte("bad"))
			assert.NilError(t, err)
			assert.NilError(t, tw.Close())

			// Untar must reject parent-traversal entries; regardless,
			// nothing may be written above dest.
			err = Untar(&buf, dest, &TarOptions{NoLchown: true})
			assert.ErrorType(t, err, &breakoutErr{})

			// dest's parent must still contain only dest.
			entries, err := os.ReadDir(base)
			assert.NilError(t, err)
			assert.Equal(t, len(entries), 1, "unexpected escape into parent: %v", entries)
			assert.Equal(t, entries[0].Name(), "dest")
		})
	}
}

// TestUntarSiblingPrefixContained verifies that a symlink whose target is a
// sibling directory sharing the destination's path prefix (dest "base/dest",
// sibling "base/dest-evil") cannot be written through. Regression test for the
// old string-prefix (HasPrefix) containment check, which treated such a sibling
// as inside the destination.
func TestUntarSiblingPrefixContained(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "dest")
	assert.NilError(t, os.Mkdir(dest, 0o755))
	// Prefix-sharing sibling with a sentinel file.
	evil := filepath.Join(base, "dest-evil")
	assert.NilError(t, os.Mkdir(evil, 0o755))

	assert.NilError(t, os.WriteFile(filepath.Join(evil, "secret"), []byte("secret"), 0o600))

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// A hardlink whose target resolves to the prefix-sharing sibling. The old
	// strings.HasPrefix check accepted this because "base/dest-evil/secret"
	// starts with "base/dest".
	assert.NilError(t, tw.WriteHeader(&tar.Header{
		Name:     "grab",
		Typeflag: tar.TypeLink,
		Linkname: "../dest-evil/secret",
		Mode:     0o644,
	}))
	assert.NilError(t, tw.Close())

	_ = Untar(&buf, dest, &TarOptions{NoLchown: true}) // may error; we only require containment

	// No hardlink to the sibling's secret may be created inside dest.
	_, statErr := os.Stat(filepath.Join(dest, "grab"))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "hardlink to prefix-sibling created")
}

func TestUntarHardlinkToSymlink(t *testing.T) {
	skip.If(t, runtime.GOOS != "windows" && os.Getuid() != 0, "skipping test that requires root")
	for i, headers := range [][]*tar.Header{
		{
			{
				Name:     "symlink1",
				Typeflag: tar.TypeSymlink,
				Linkname: "regfile",
				Mode:     0o644,
			},
			{
				Name:     "symlink2",
				Typeflag: tar.TypeLink,
				Linkname: "symlink1",
				Mode:     0o644,
			},
			{
				Name:     "regfile",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
			},
		},
	} {
		if err := testBreakout("untar", "docker-TestUntarHardlinkToSymlink", headers); err != nil {
			t.Fatalf("i=%d. %v", i, err)
		}
	}
}

func TestUntarInvalidHardlink(t *testing.T) {
	for i, headers := range [][]*tar.Header{
		{ // try reading victim/hello (../)
			{
				Name:     "dotdot",
				Typeflag: tar.TypeLink,
				Linkname: "../victim/hello",
				Mode:     0o644,
			},
		},
		{ // try reading victim/hello (/../)
			{
				Name:     "slash-dotdot",
				Typeflag: tar.TypeLink,
				// Note the leading slash
				Linkname: "/../victim/hello",
				Mode:     0o644,
			},
		},
		{ // try writing victim/file
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeLink,
				Linkname: "../victim",
				Mode:     0o755,
			},
			{
				Name:     "loophole-victim/file",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
			},
		},
		{ // try reading victim/hello (hardlink, symlink)
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeLink,
				Linkname: "../victim",
				Mode:     0o755,
			},
			{
				Name:     "symlink",
				Typeflag: tar.TypeSymlink,
				Linkname: "loophole-victim/hello",
				Mode:     0o644,
			},
		},
		{ // Try reading victim/hello (hardlink, hardlink)
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeLink,
				Linkname: "../victim",
				Mode:     0o755,
			},
			{
				Name:     "hardlink",
				Typeflag: tar.TypeLink,
				Linkname: "loophole-victim/hello",
				Mode:     0o644,
			},
		},
		{ // Try removing victim directory (hardlink)
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeLink,
				Linkname: "../victim",
				Mode:     0o755,
			},
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
			},
		},
	} {
		if err := testBreakout("untar", "docker-TestUntarInvalidHardlink", headers); err != nil {
			t.Fatalf("i=%d. %v", i, err)
		}
	}
}

func TestUntarInvalidSymlink(t *testing.T) {
	for i, headers := range [][]*tar.Header{
		{ // try reading victim/hello (../)
			{
				Name:     "dotdot",
				Typeflag: tar.TypeSymlink,
				Linkname: "../victim/hello",
				Mode:     0o644,
			},
		},
		{ // try reading victim/hello (/../)
			{
				Name:     "slash-dotdot",
				Typeflag: tar.TypeSymlink,
				// Note the leading slash
				Linkname: "/../victim/hello",
				Mode:     0o644,
			},
		},
		{ // try writing victim/file
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeSymlink,
				Linkname: "../victim",
				Mode:     0o755,
			},
			{
				Name:     "loophole-victim/file",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
			},
		},
		{ // try reading victim/hello (symlink, symlink)
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeSymlink,
				Linkname: "../victim",
				Mode:     0o755,
			},
			{
				Name:     "symlink",
				Typeflag: tar.TypeSymlink,
				Linkname: "loophole-victim/hello",
				Mode:     0o644,
			},
		},
		{ // try reading victim/hello (symlink, hardlink)
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeSymlink,
				Linkname: "../victim",
				Mode:     0o755,
			},
			{
				Name:     "hardlink",
				Typeflag: tar.TypeLink,
				Linkname: "loophole-victim/hello",
				Mode:     0o644,
			},
		},
		{ // try removing victim directory (symlink)
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeSymlink,
				Linkname: "../victim",
				Mode:     0o755,
			},
			{
				Name:     "loophole-victim",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
			},
		},
		{ // try writing to victim/newdir/newfile with a symlink in the path
			{
				// this header needs to be before the next one, or else there is an error
				Name:     "dir/loophole",
				Typeflag: tar.TypeSymlink,
				Linkname: "../../victim",
				Mode:     0o755,
			},
			{
				Name:     "dir/loophole/newdir/newfile",
				Typeflag: tar.TypeReg,
				Mode:     0o644,
			},
		},
	} {
		if err := testBreakout("untar", "docker-TestUntarInvalidSymlink", headers); err != nil {
			t.Fatalf("i=%d. %v", i, err)
		}
	}
}

// TestUntarSymlinkBreakout is a regression test for a tar path-traversal
// vulnerability: a two-hop symlink chain in a malicious archive can escape
// the extraction root at runtime while passing the static path checks that
// guard each entry name and symlink target.  Two hops are needed because a
// direct out-of-root symlink target is already rejected by a static check in
// createTarFile; the first hop (go_up -> "..") fools that check for the
// second hop (escape -> "../victim") by appearing to stay within the root
// when paths are joined as strings, while the OS resolves go_up at runtime
// and places escape one level higher than the check assumed.
func TestUntarSymlinkBreakout(t *testing.T) {
	tmpdir := t.TempDir()
	dest := filepath.Join(tmpdir, "dest")
	victim := filepath.Join(tmpdir, "victim")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(victim, 0o755); err != nil {
		t.Fatal(err)
	}

	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	for _, hdr := range []*tar.Header{
		{Name: "inner", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "inner/go_up", Typeflag: tar.TypeSymlink, Linkname: ".."},
		{Name: "inner/go_up/escape", Typeflag: tar.TypeSymlink, Linkname: "../victim"},
		{Name: "inner/go_up/escape/newfile", Typeflag: tar.TypeReg, Mode: 0o644},
	} {
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()

	// Ignore any extraction error: a breakoutError means the escape was
	// caught; no error means the write was safely redirected within dest.
	// NoLchown suppresses the ownership call so the test runs without root.
	_ = Untar(buf, dest, &TarOptions{NoLchown: true})

	// victim/newfile must not exist; its presence proves a breakout.
	if _, err := os.Lstat(filepath.Join(victim, "newfile")); err == nil {
		t.Fatal("archive breakout: newfile was written outside extraction root via symlink chain")
	}
}

func TestTempArchiveCloseMultipleTimes(t *testing.T) {
	reader := io.NopCloser(strings.NewReader("hello"))
	tmpArchive, err := newTempArchive(reader, "")
	assert.NilError(t, err)
	buf := make([]byte, 10)
	n, err := tmpArchive.Read(buf)
	assert.NilError(t, err)
	if n != 5 {
		t.Fatalf("Expected to read 5 bytes. Read %d instead", n)
	}
	for i := range 3 {
		if err = tmpArchive.Close(); err != nil {
			t.Fatalf("i=%d. Unexpected error closing temp archive: %v", i, err)
		}
	}
}

// TestXGlobalNoParent is a regression test to check parent directories are not created for PAX headers
func TestXGlobalNoParent(t *testing.T) {
	buf := &bytes.Buffer{}
	w := tar.NewWriter(buf)
	err := w.WriteHeader(&tar.Header{
		Name:     "foo/bar",
		Typeflag: tar.TypeXGlobalHeader,
	})
	assert.NilError(t, err)
	tmpDir := t.TempDir()
	err = Untar(buf, tmpDir, nil)
	assert.NilError(t, err)

	_, err = os.Lstat(filepath.Join(tmpDir, "foo"))
	assert.Check(t, err != nil)
	assert.Check(t, is.ErrorIs(err, os.ErrNotExist))
}

func TestReplaceFileTarWrapper(t *testing.T) {
	filesInArchive := 20
	tests := []struct {
		doc       string
		filename  string
		modifier  TarModifierFunc
		expected  string
		fileCount int
	}{
		{
			doc:       "Modifier creates a new file",
			filename:  "newfile",
			modifier:  createModifier(t),
			expected:  "the new content",
			fileCount: filesInArchive + 1,
		},
		{
			doc:       "Modifier replaces a file",
			filename:  "file-2",
			modifier:  createOrReplaceModifier,
			expected:  "the new content",
			fileCount: filesInArchive,
		},
		{
			doc:       "Modifier replaces the last file",
			filename:  fmt.Sprintf("file-%d", filesInArchive-1),
			modifier:  createOrReplaceModifier,
			expected:  "the new content",
			fileCount: filesInArchive,
		},
		{
			doc:       "Modifier appends to a file",
			filename:  "file-3",
			modifier:  appendModifier,
			expected:  "fooo\nnext line",
			fileCount: filesInArchive,
		},
	}

	for _, tc := range tests {
		sourceArchive := buildSourceArchive(t, filesInArchive)
		defer sourceArchive.Close()

		resultArchive := ReplaceFileTarWrapper(
			sourceArchive,
			map[string]TarModifierFunc{tc.filename: tc.modifier})

		actual := readFileFromArchive(t, resultArchive, tc.filename, tc.fileCount, tc.doc)
		assert.Check(t, is.Equal(tc.expected, actual), tc.doc)
	}
}

// TestPrefixHeaderReadable tests that files that could be created with the
// version of this package that was built with <=go17 are still readable.
func TestPrefixHeaderReadable(t *testing.T) {
	skip.If(t, runtime.GOOS != "windows" && os.Getuid() != 0, "skipping test that requires root")
	skip.If(t, userns.RunningInUserNS(), "skipping test that requires more than 010000000 UIDs, which is unlikely to be satisfied when running in userns")
	// https://gist.github.com/stevvooe/e2a790ad4e97425896206c0816e1a882#file-out-go
	testFile := []byte("\x1f\x8b\x08\x08\x44\x21\x68\x59\x00\x03\x74\x2e\x74\x61\x72\x00\x4b\xcb\xcf\x67\xa0\x35\x30\x80\x00\x86\x06\x10\x47\x01\xc1\x37\x40\x00\x54\xb6\xb1\xa1\xa9\x99\x09\x48\x25\x1d\x40\x69\x71\x49\x62\x91\x02\xe5\x76\xa1\x79\x84\x21\x91\xd6\x80\x72\xaf\x8f\x82\x51\x30\x0a\x46\x36\x00\x00\xf0\x1c\x1e\x95\x00\x06\x00\x00")

	tmpDir := t.TempDir()
	err := Untar(bytes.NewReader(testFile), tmpDir, nil)
	assert.NilError(t, err)

	baseName := "foo"
	pth := strings.Repeat("a", 100-len(baseName)) + "/" + baseName

	_, err = os.Lstat(filepath.Join(tmpDir, pth))
	assert.NilError(t, err)
}

func buildSourceArchive(t *testing.T, numberOfFiles int) io.ReadCloser {
	srcDir, err := os.MkdirTemp(t.TempDir(), "docker-test-srcDir")
	assert.NilError(t, err)

	_, err = prepareUntarSourceDirectory(numberOfFiles, srcDir, false)
	assert.NilError(t, err)

	sourceArchive, err := TarWithOptions(srcDir, &TarOptions{})
	assert.NilError(t, err)
	return sourceArchive
}

func createOrReplaceModifier(path string, header *tar.Header, content io.Reader) (*tar.Header, []byte, error) {
	return &tar.Header{
		Mode:     0o600,
		Typeflag: tar.TypeReg,
	}, []byte("the new content"), nil
}

func createModifier(t *testing.T) TarModifierFunc {
	return func(path string, header *tar.Header, content io.Reader) (*tar.Header, []byte, error) {
		assert.Check(t, is.Nil(content))
		return createOrReplaceModifier(path, header, content)
	}
}

func appendModifier(path string, header *tar.Header, content io.Reader) (*tar.Header, []byte, error) {
	buffer := bytes.Buffer{}
	if content != nil {
		if _, err := buffer.ReadFrom(content); err != nil {
			return nil, nil, err
		}
	}
	buffer.WriteString("\nnext line")
	return &tar.Header{Mode: 0o600, Typeflag: tar.TypeReg}, buffer.Bytes(), nil
}

func readFileFromArchive(t *testing.T, archive io.ReadCloser, name string, expectedCount int, doc string) string {
	skip.If(t, runtime.GOOS != "windows" && os.Getuid() != 0, "skipping test that requires root")
	destDir := t.TempDir()

	err := Untar(archive, destDir, nil)
	assert.NilError(t, err)

	files, _ := os.ReadDir(destDir)
	assert.Check(t, is.Len(files, expectedCount), doc)

	content, err := os.ReadFile(filepath.Join(destDir, name))
	assert.Check(t, err)
	return string(content)
}
