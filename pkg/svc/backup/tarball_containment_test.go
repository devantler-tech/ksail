package backup_test

import (
	"archive/tar"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/backup"
	"github.com/stretchr/testify/require"
)

// escapeCandidates lists the directories a traversing entry can reach from the
// extraction directory, which os.MkdirTemp places directly under os.TempDir().
// One "../" therefore lands in os.TempDir() itself and two in its parent; a long
// run of "../" clamps at the filesystem root, so /tmp is reachable too. These are
// exactly the locations an unconfined extractor was observed writing to.
func escapeCandidates() []string {
	tempDir := os.TempDir()

	return []string{tempDir, filepath.Dir(tempDir), "/tmp", string(filepath.Separator)}
}

// findEscapedMarker reports every path outside dir at which marker exists.
//
// It deliberately runs whether or not extraction returned an error: entries are
// written to disk as the archive is walked, so a malicious entry can land before
// a later failure aborts extraction. An oracle that returns early on error is
// blind to precisely the case this test exists to catch.
func findEscapedMarker(t *testing.T, marker string) []string {
	t.Helper()

	var escaped []string

	for _, candidate := range escapeCandidates() {
		path := filepath.Join(candidate, marker)

		_, err := os.Lstat(path)
		if err == nil {
			escaped = append(escaped, path)
		}
	}

	return escaped
}

// TestExtractBackupArchive_ContainsWritesToDestDir asserts at the filesystem
// level that a traversing entry never writes outside the extraction directory.
//
// This complements TestExtractBackupArchive_SecurityGuards, which asserts the
// sentinel error a rejected entry produces. That is a statement about the error
// value; this is a statement about the disk, and only the latter fails if a
// future refactor keeps returning an error while still writing the file.
func TestExtractBackupArchive_ContainsWritesToDestDir(t *testing.T) {
	t.Parallel()

	validMeta := `{"version":"v1","clusterName":"test-cluster","resourceCount":1}`

	// A marker unique to this run: these vectors write to shared directories, so
	// a fixed name would let one run's leftovers be read as another run's escape.
	nonce := fmt.Sprintf("%d%d", os.Getpid(), time.Now().UnixNano()%1_000_000)

	tests := []struct {
		name     string
		typeflag byte
		prefix   string
	}{
		{name: "parent traversal", typeflag: tar.TypeReg, prefix: "../"},
		{name: "grandparent traversal", typeflag: tar.TypeReg, prefix: "../../"},
		{name: "dot slash traversal", typeflag: tar.TypeReg, prefix: "./../../"},
		{name: "embedded traversal", typeflag: tar.TypeReg, prefix: "resources/../../"},
		{name: "root clamped traversal", typeflag: tar.TypeReg, prefix: strings.Repeat("../", 12)},
		{name: "directory traversal", typeflag: tar.TypeDir, prefix: "../"},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			marker := fmt.Sprintf("ksail-containment-%s-%d", nonce, index)

			var content []byte
			if test.typeflag == tar.TypeReg {
				content = []byte("escaped")
			}

			archivePath := createMaliciousArchive(t, validMeta, tar.Header{
				Name:     test.prefix + marker,
				Typeflag: test.typeflag,
				Size:     int64(len(content)),
				Mode:     0o600,
			}, content)

			dir, _, err := backup.ExtractBackupArchive(archivePath)
			if dir != "" {
				t.Cleanup(func() { _ = os.RemoveAll(dir) })
			}

			escaped := findEscapedMarker(t, marker)
			for _, path := range escaped {
				t.Cleanup(func() { _ = os.RemoveAll(path) })
			}

			require.Emptyf(t, escaped,
				"entry %q escaped the extraction directory (extract err: %v)",
				test.prefix+marker, err,
			)
		})
	}
}

// tarWithEntry builds an in-memory tar containing a single regular-file entry.
func tarWithEntry(t *testing.T, name string, content []byte) *tar.Reader {
	t.Helper()

	var buf bytes.Buffer

	writer := tar.NewWriter(&buf)
	require.NoError(t, writer.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Size:     int64(len(content)),
		Mode:     0o600,
	}))
	_, err := writer.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	return tar.NewReader(&buf)
}

// TestExtractTarEntries_DoesNotFollowSymlinkInDestDir covers the case the
// traversal test above structurally cannot: an entry whose path is entirely
// legitimate.
//
// "resources/evidence" contains no "..", is not absolute, and resolves inside
// the destination, so validateTarEntry accepts it. If "resources" is a symlink
// pointing outside the destination, a plain os.MkdirAll/os.OpenFile pair follows
// it and writes through — lexical validation cannot see this, because the path
// is only dangerous once the filesystem resolves it. Extracting through a root
// refuses to cross the symlink.
//
// This is what pins the root: every traversal vector above is rejected by
// validateTarEntry before extraction runs, so removing the root would leave them
// all still passing.
func TestExtractTarEntries_DoesNotFollowSymlinkInDestDir(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()
	outside := t.TempDir()

	// A pre-existing symlink inside the destination, aimed out of it.
	require.NoError(t, os.Symlink(outside, filepath.Join(destDir, "resources")))

	const marker = "evidence"

	entry := filepath.Join("resources", marker)
	reader := tarWithEntry(t, entry, []byte("escaped"))

	// The entry itself must be one lexical validation accepts, or this test
	// would prove nothing beyond what the traversal cases already cover.
	_, err := backup.ValidateTarEntry(
		&tar.Header{Name: entry, Typeflag: tar.TypeReg}, destDir,
	)
	require.NoError(t, err, "precondition: the entry must pass lexical validation")

	extractErr := backup.ExtractTarEntries(reader, destDir)

	// Whether extraction reports an error is not the assertion: what matters is
	// that nothing was written through the symlink.
	_, statErr := os.Lstat(filepath.Join(outside, marker))
	require.Truef(t, os.IsNotExist(statErr),
		"entry %q was written through a symlink to outside the destination (extract err: %v)",
		entry, extractErr,
	)
}
