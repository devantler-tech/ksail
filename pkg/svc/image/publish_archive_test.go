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

// TestPublishArchiveRenamesWhenPossible checks the atomic path.
func TestPublishArchiveRenamesWhenPossible(t *testing.T) {
	t.Parallel()

	staged, output := writePublishFixture(t)

	require.NoError(t, image.PublishArchiveForTest(staged, output, os.Rename))

	data, err := os.ReadFile(output) //nolint:gosec // Test fixture path.
	require.NoError(t, err)
	require.Equal(t, "new archive", string(data))
	require.NoFileExists(t, staged)
}

// TestPublishArchiveCopiesIntoUnrenameableDestination covers a bind-mounted
// output file (EBUSY) and a destination on another filesystem (EXDEV).
func TestPublishArchiveCopiesIntoUnrenameableDestination(t *testing.T) {
	t.Parallel()

	for _, errno := range []syscall.Errno{syscall.EBUSY, syscall.EXDEV} {
		t.Run(errno.Error(), func(t *testing.T) {
			t.Parallel()

			staged, output := writePublishFixture(t)

			require.NoError(
				t,
				image.PublishArchiveForTest(staged, output, refuseRenameOnto(output, errno)),
			)

			data, err := os.ReadFile(output) //nolint:gosec // Test fixture path.
			require.NoError(t, err)
			require.Equal(t, "new archive", string(data))
		})
	}
}

// TestPublishArchiveKeepsArchiveWhenCopyFails checks that a completed export is
// moved next to the destination, and named, when it cannot be copied in.
func TestPublishArchiveKeepsArchiveWhenCopyFails(t *testing.T) {
	t.Parallel()

	// A directory cannot be opened for writing, even by root, so the copy fails.
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged.tar")
	output := filepath.Join(dir, "images.tar")

	require.NoError(t, os.WriteFile(staged, []byte("new archive"), 0o600))
	require.NoError(t, os.Mkdir(output, 0o700))

	err := image.PublishArchiveForTest(staged, output, refuseRenameOnto(output, syscall.EBUSY))
	require.Error(t, err)
	require.ErrorContains(t, err, output+".ksail-export")

	kept, readErr := os.ReadFile(output + ".ksail-export") //nolint:gosec // Test fixture path.
	require.NoError(t, readErr)
	require.Equal(t, "new archive", string(kept))

	require.DirExists(t, output)
}

// TestPublishArchiveReturnsOtherRenameErrors checks that only EBUSY and EXDEV
// fall back to copying.
func TestPublishArchiveReturnsOtherRenameErrors(t *testing.T) {
	t.Parallel()

	staged, output := writePublishFixture(t)

	err := image.PublishArchiveForTest(staged, output, refuseRenameOnto(output, syscall.EACCES))
	require.ErrorIs(t, err, syscall.EACCES)
	require.NotErrorIs(t, err, syscall.EBUSY)

	previous, readErr := os.ReadFile(output) //nolint:gosec // Test fixture path.
	require.NoError(t, readErr)
	require.Equal(t, "old archive contents", string(previous))
	require.FileExists(t, staged)
}
