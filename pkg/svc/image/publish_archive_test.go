package image_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/stretchr/testify/require"
)

// writePublishFixture creates a staged archive and an existing destination.
func writePublishFixture(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	staged := filepath.Join(dir, "staged.tar")
	output := filepath.Join(dir, "images.tar")

	require.NoError(t, os.WriteFile(staged, []byte("new archive"), 0o600))
	require.NoError(t, os.WriteFile(output, []byte("old archive contents"), 0o600))

	return staged, output
}

// refuseRenameOnto returns a rename that fails with errno only when renaming onto target.
func refuseRenameOnto(target string, errno syscall.Errno) func(string, string) error {
	return func(from, to string) error {
		if to == target {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: errno}
		}

		return os.Rename(from, to)
	}
}

// readFixture reads a test file.
func readFixture(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // Test fixture path.
	require.NoError(t, err)

	return string(data)
}

// keptArchives lists the archives kept beside output.
func keptArchives(t *testing.T, output string) []string {
	t.Helper()

	matches, err := filepath.Glob(output + ".*.ksail-export")
	require.NoError(t, err)

	return matches
}

// TestPublishArchiveRenamesWhenPossible checks the atomic path.
func TestPublishArchiveRenamesWhenPossible(t *testing.T) {
	t.Parallel()

	staged, output := writePublishFixture(t)

	require.NoError(t, image.PublishArchiveForTest(staged, output, os.Rename))
	require.Equal(t, "new archive", readFixture(t, output))
	require.NoFileExists(t, staged)
	require.Empty(t, keptArchives(t, output))
}

// TestPublishArchiveKeepsArchiveWhenDestinationRefusesRename covers a
// bind-mounted output (EBUSY), another filesystem (EXDEV) and a protected
// directory (EPERM, EACCES): the previous archive is never touched, and the
// completed one is kept beside it and named in the error.
func TestPublishArchiveKeepsArchiveWhenDestinationRefusesRename(t *testing.T) {
	t.Parallel()

	for _, errno := range []syscall.Errno{syscall.EBUSY, syscall.EXDEV, syscall.EPERM, syscall.EACCES} {
		t.Run(errno.Error(), func(t *testing.T) {
			t.Parallel()

			staged, output := writePublishFixture(t)

			err := image.PublishArchiveForTest(staged, output, refuseRenameOnto(output, errno))
			require.ErrorIs(t, err, errno)

			kept := keptArchives(t, output)
			require.Len(t, kept, 1)
			require.ErrorContains(t, err, kept[0])
			require.Equal(t, "new archive", readFixture(t, kept[0]))
			require.Equal(t, "old archive contents", readFixture(t, output))
			require.NoFileExists(t, staged)
		})
	}
}

// TestPublishArchiveNeverReplacesAnEarlierKeptArchive checks that two failed
// publications to the same destination keep both completed archives.
func TestPublishArchiveNeverReplacesAnEarlierKeptArchive(t *testing.T) {
	t.Parallel()

	staged, output := writePublishFixture(t)
	refuse := refuseRenameOnto(output, syscall.EBUSY)

	require.Error(t, image.PublishArchiveForTest(staged, output, refuse))

	require.NoError(t, os.WriteFile(staged, []byte("second archive"), 0o600))
	require.Error(t, image.PublishArchiveForTest(staged, output, refuse))

	kept := keptArchives(t, output)
	require.Len(t, kept, 2)

	contents := []string{readFixture(t, kept[0]), readFixture(t, kept[1])}
	require.ElementsMatch(t, []string{"new archive", "second archive"}, contents)
}

func TestPublishArchiveRetainsStagingWhenReservationFails(t *testing.T) {
	t.Parallel()

	output := filepath.Join(t.TempDir(), "missing", "images.tar")
	rename := func(_, _ string) error { return syscall.EACCES }

	assertRetainedAfterCleanup(t, output, rename, syscall.EACCES, os.ErrNotExist)
}

func TestPublishArchiveRetainsStagingWhenBothRenamesFail(t *testing.T) {
	t.Parallel()

	_, output := writePublishFixture(t)
	rename := func(_, to string) error {
		if to == output {
			return syscall.EBUSY
		}

		return syscall.EPERM
	}

	assertRetainedAfterCleanup(t, output, rename, syscall.EBUSY, syscall.EPERM)
	require.Equal(t, "old archive contents", readFixture(t, output))
	require.Empty(t, keptArchives(t, output))
}

func assertRetainedAfterCleanup(
	t *testing.T, output string, rename func(string, string) error, causes ...error,
) {
	t.Helper()

	directory := t.TempDir()
	staged := filepath.Join(directory, "combined.tar")
	content := filepath.Join(directory, "content")
	require.NoError(t, os.WriteFile(staged, []byte("new archive"), 0o600))
	require.NoError(t, os.Mkdir(content, 0o700))

	root, err := os.OpenRoot(directory)
	require.NoError(t, err)

	publishErr := image.PublishArchiveForTest(staged, output, rename)
	err = image.CleanupExportSourcesForTest(t.Context(), root, directory, publishErr)
	require.Equal(t, "new archive", readFixture(t, staged))
	require.ErrorContains(t, err, staged)
	require.NoDirExists(t, content)

	for _, cause := range causes {
		require.ErrorIs(t, err, cause)
	}
}
