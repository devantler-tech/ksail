package compression

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestExtension(t *testing.T) {
	tests := []struct {
		compression Compression
		extension   string
	}{
		{compression: -1, extension: ""},
		{compression: None, extension: "tar"},
		{compression: Bzip2, extension: "tar.bz2"},
		{compression: Gzip, extension: "tar.gz"},
		{compression: Xz, extension: "tar.xz"},
		{compression: Zstd, extension: "tar.zst"},
	}
	for _, tc := range tests {
		if actual := tc.compression.Extension(); actual != tc.extension {
			t.Errorf("expected %s extension got %s", tc.extension, actual)
		}
	}
}

func TestDetectCompressionZstd(t *testing.T) {
	// test zstd compression without skippable frames.
	compressedData := []byte{
		0x28, 0xb5, 0x2f, 0xfd, // magic number of Zstandard frame: 0xFD2FB528
		0x04, 0x00, 0x31, 0x00, 0x00, // frame header
		0x64, 0x6f, 0x63, 0x6b, 0x65, 0x72, // data block "docker"
		0x16, 0x0e, 0x21, 0xc3, // content checksum
	}
	compression := Detect(compressedData)
	if compression != Zstd {
		t.Fatal("Unexpected compression")
	}
	// test zstd compression with skippable frames.
	hex := []byte{
		0x50, 0x2a, 0x4d, 0x18, // magic number of skippable frame: 0x184D2A50 to 0x184D2A5F
		0x04, 0x00, 0x00, 0x00, // frame size
		0x5d, 0x00, 0x00, 0x00, // user data
		0x28, 0xb5, 0x2f, 0xfd, // magic number of Zstandard frame: 0xFD2FB528
		0x04, 0x00, 0x31, 0x00, 0x00, // frame header
		0x64, 0x6f, 0x63, 0x6b, 0x65, 0x72, // data block "docker"
		0x16, 0x0e, 0x21, 0xc3, // content checksum
	}
	compression = Detect(hex)
	if compression != Zstd {
		t.Fatal("Unexpected compression")
	}
}

// toUnixPath converts the given path to a unix-path, using forward-slashes, and
// with the drive-letter replaced (e.g. "C:\temp\file.txt" becomes "/c/temp/file.txt").
// It is a no-op on non-Windows platforms.
func toUnixPath(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	p = filepath.ToSlash(p)

	vol := strings.TrimPrefix(filepath.VolumeName(p), "//?/")
	if len(vol) == 2 && vol[1] == ':' {
		return "/" + strings.ToLower(vol[:1]) + p[len(filepath.VolumeName(p)):]
	}
	return p
}

func testDecompressStream(t *testing.T, ext, compressCommand string) io.Reader {
	tmp := t.TempDir()
	archivePath := toUnixPath(filepath.Join(tmp, "archive"))
	cmd := exec.Command("sh", "-c", fmt.Sprintf("touch %[1]s && %[2]s %[1]s", archivePath, compressCommand))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to create archive file (%v):\ncommand: %s\noutput: %s", err, cmd.String(), output)
	}
	filename := "archive." + ext
	archive, err := os.Open(filepath.Join(tmp, filename))
	if err != nil {
		t.Fatalf("Failed to open file %s: %v", filename, err)
	}
	defer archive.Close()

	r, err := DecompressStream(archive)
	if err != nil {
		t.Fatalf("Failed to decompress %s: %v", filename, err)
	}
	if _, err = io.ReadAll(r); err != nil {
		t.Fatalf("Failed to read the decompressed stream: %v ", err)
	}
	if err = r.Close(); err != nil {
		t.Fatalf("Failed to close the decompressed stream: %v ", err)
	}

	return r
}

func TestDecompressStreamGzip(t *testing.T) {
	testDecompressStream(t, "gz", "gzip -f")
}

func TestDecompressStreamBzip2(t *testing.T) {
	// TODO Windows: Failing with "bzip2.exe: Can't open input file (...)/archive: No such file or directory."
	if runtime.GOOS == "windows" {
		t.Skip("Failing on Windows CI machines")
	}
	testDecompressStream(t, "bz2", "bzip2 -f")
}

func TestDecompressStreamXz(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Xz not present in msys2")
	}
	testDecompressStream(t, "xz", "xz -f")
}

func TestDecompressStreamZstd(t *testing.T) {
	// TODO Windows: Failing with "zstd: can't stat (...)/archive : No such file or directory -- ignored"
	if runtime.GOOS == "windows" {
		t.Skip("Failing on Windows CI machines")
	}
	if _, err := exec.LookPath("zstd"); err != nil {
		t.Skip("zstd not installed")
	}
	testDecompressStream(t, "zst", "zstd -f")
}

func TestCompressStreamXzUnsupported(t *testing.T) {
	dest, err := os.Create(filepath.Join(t.TempDir(), "dest"))
	if err != nil {
		t.Fatalf("Fail to create the destination file")
	}
	defer dest.Close()

	_, err = CompressStream(dest, Xz)
	if err == nil {
		t.Fatalf("Should fail as xz is unsupported for compression format.")
	}
}

func TestCompressStreamBzip2Unsupported(t *testing.T) {
	dest, err := os.Create(filepath.Join(t.TempDir(), "dest"))
	if err != nil {
		t.Fatalf("Fail to create the destination file")
	}
	defer dest.Close()

	_, err = CompressStream(dest, Bzip2)
	if err == nil {
		t.Fatalf("Should fail as bzip2 is unsupported for compression format.")
	}
}

func TestCompressStreamInvalid(t *testing.T) {
	dest, err := os.Create(filepath.Join(t.TempDir(), "dest"))
	if err != nil {
		t.Fatalf("Fail to create the destination file")
	}
	defer dest.Close()

	_, err = CompressStream(dest, -1)
	if err == nil {
		t.Fatalf("Should fail as xz is unsupported for compression format.")
	}
}

func TestCmdStreamLargeStderr(t *testing.T) {
	cmd := exec.Command("sh", "-c", "dd if=/dev/zero bs=1k count=1000 of=/dev/stderr; echo hello")
	out, err := cmdStream(cmd, nil)
	if err != nil {
		t.Fatalf("Failed to start command: %s, output: %s", err, out)
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, out)
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Command should not have failed (err=%.100s...)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Command did not complete in 5 seconds; probable deadlock")
	}
}

func TestCmdStreamBad(t *testing.T) {
	// TODO Windows: Figure out why this is failing in CI but not locally
	if runtime.GOOS == "windows" {
		t.Skip("Failing on Windows CI machines")
	}
	badCmd := exec.Command("sh", "-c", "echo hello; echo >&2 error couldn\\'t reverse the phase pulser; exit 1")
	out, err := cmdStream(badCmd, nil)
	if err != nil {
		t.Fatalf("Failed to start command: %s", err)
	}
	if output, err := io.ReadAll(out); err == nil {
		t.Fatalf("Command should have failed")
	} else if err.Error() != "exit status 1: error couldn't reverse the phase pulser\n" {
		t.Fatalf("Wrong error value (%s)", err)
	} else if s := string(output); s != "hello\n" {
		t.Fatalf("Command output should be '%s', not '%s'", "hello\\n", output)
	}
}

func TestCmdStreamGood(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo hello; exit 0")
	out, err := cmdStream(cmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := io.ReadAll(out); err != nil {
		t.Fatalf("Command should not have failed (err=%s)", err)
	} else if s := string(output); s != "hello\n" {
		t.Fatalf("Command output should be '%s', not '%s'", "hello\\n", output)
	}
}

func TestDisablePigz(t *testing.T) {
	_, err := exec.LookPath("unpigz")
	if err != nil {
		t.Log("Test will not check full path when Pigz not installed")
	}

	t.Setenv("MOBY_DISABLE_PIGZ", "true")

	r := testDecompressStream(t, "gz", "gzip -f")

	// wrapped in closer to cancel contex and release buffer to pool
	wrapper := r.(*readCloserWrapper)

	assert.Equal(t, reflect.TypeOf(wrapper.Reader), reflect.TypeFor[*gzip.Reader]())
}

func TestPigz(t *testing.T) {
	r := testDecompressStream(t, "gz", "gzip -f")
	// wrapper for buffered reader and context cancel
	wrapper := r.(*readCloserWrapper)

	_, err := exec.LookPath("unpigz")
	if err == nil {
		t.Log("Tested whether Pigz is used, as it installed")
		// For the command wait wrapper
		cmdWaitCloserWrapper := wrapper.Reader.(*readCloserWrapper)
		assert.Equal(t, reflect.TypeOf(cmdWaitCloserWrapper.Reader), reflect.TypeFor[*io.PipeReader]())
	} else {
		t.Log("Tested whether Pigz is not used, as it not installed")
		assert.Equal(t, reflect.TypeOf(wrapper.Reader), reflect.TypeFor[*gzip.Reader]())
	}
}
