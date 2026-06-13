package workloadwatch

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// PollInterval is the time between file modification time scans. Acts as a
// safety net for environments where inotify may miss events (CI runners under
// high I/O load, editors using atomic save via create+rename, etc.).
const PollInterval = 3 * time.Second

// FileSnapshot maps file paths to their last-known modification time.
// Used by the polling fallback to detect changes missed by fsnotify.
type FileSnapshot map[string]time.Time

// BuildFileSnapshot walks the directory tree and records modification times
// for all regular files. Uses os.Stat instead of d.Info() to avoid stale
// cached stat data when files are replaced via rename (e.g. sed -i).
func BuildFileSnapshot(dir string) FileSnapshot {
	snap := make(FileSnapshot)

	_ = filepath.WalkDir(dir, func(path string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil || dirEntry.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil //nolint:nilerr // skip non-regular entries and stat errors
		}

		snap[path] = info.ModTime()

		return nil
	})

	return snap
}

// DetectChangedFile scans the directory for a file whose modification time
// differs from the snapshot. Returns the first changed file path found and
// updates the snapshot in place. Returns "" if no changes are detected.
func DetectChangedFile(dir string, snapshot FileSnapshot) string {
	changed := scanForModifiedFiles(dir, snapshot)
	deleted := scanForDeletedFiles(snapshot)

	if changed != "" {
		return changed
	}

	return deleted
}

// scanForModifiedFiles walks the directory tree and returns the first file
// whose modification time differs from the snapshot. Updates the snapshot
// in place for all changed files encountered during the walk.
func scanForModifiedFiles(dir string, snapshot FileSnapshot) string {
	var changed string

	_ = filepath.WalkDir(dir, func(path string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil || dirEntry.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil //nolint:nilerr // skip non-regular entries and stat errors
		}

		modTime := info.ModTime()
		if prev, ok := snapshot[path]; !ok || !modTime.Equal(prev) {
			snapshot[path] = modTime

			if changed == "" {
				changed = path
			}
		}

		return nil
	})

	return changed
}

// scanForDeletedFiles checks all snapshot entries and removes any whose
// path is missing or is no longer a regular file. Returns the first
// deleted path found, or "".
func scanForDeletedFiles(snapshot FileSnapshot) string {
	var changed string

	for path := range snapshot {
		info, statErr := os.Lstat(path)

		if statErr != nil || !info.Mode().IsRegular() {
			delete(snapshot, path)

			if changed == "" {
				changed = path
			}
		}
	}

	return changed
}

// PollForChanges periodically scans the watched directory for modified files
// and enqueues applies directly on applyCh. This provides a fallback for
// environments where fsnotify events may be lost (CI runners, atomic-save
// editors using create+rename).
//
// Unlike the fsnotify path, polling bypasses the shared debounce state
// entirely. The polling interval (3s) already provides natural debouncing,
// and a blocking send guarantees at least one apply runs for the detected
// change. A later fsnotify event may still coalesce with it before it is
// applied (EnqueueIfCurrent drains applyCh and the apply reflects the
// newest state), so the guarantee is "an apply will happen", not
// one-for-one delivery of each individual poll event.
//
// When debugWriter is non-nil, change/enqueue diagnostics are written to it.
func PollForChanges(ctx context.Context, dir string, applyCh chan string, debugWriter io.Writer) {
	snapshot := BuildFileSnapshot(dir)

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !pollHandleChange(ctx, dir, snapshot, applyCh, debugWriter) {
				return
			}
		}
	}
}

// pollHandleChange detects and enqueues a changed file for the apply worker.
// Returns false if the context was cancelled during the blocking send.
func pollHandleChange(
	ctx context.Context,
	dir string,
	snapshot FileSnapshot,
	applyCh chan string,
	debugWriter io.Writer,
) bool {
	changed := DetectChangedFile(dir, snapshot)
	if changed == "" {
		return true
	}

	if debugWriter != nil {
		_, _ = fmt.Fprintf(debugWriter, "  poll: change: %s\n", changed)
	}

	// Blocking send: guaranteed delivery to the apply worker.
	select {
	case applyCh <- changed:
		if debugWriter != nil {
			_, _ = fmt.Fprintf(debugWriter, "  poll: enqueued for apply\n")
		}

		return true
	case <-ctx.Done():
		return false
	}
}
