package workloadwatch

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// IsRelevantEvent returns true for write, create, remove, and rename events.
func IsRelevantEvent(event fsnotify.Event) bool {
	return event.Has(fsnotify.Write) ||
		event.Has(fsnotify.Create) ||
		event.Has(fsnotify.Remove) ||
		event.Has(fsnotify.Rename)
}

// AddRecursive walks the directory tree and adds all directories to the watcher.
func AddRecursive(watcher *fsnotify.Watcher, root string) error {
	err := filepath.WalkDir(root, func(path string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if dirEntry.IsDir() {
			watchErr := watcher.Add(path)
			if watchErr != nil {
				return fmt.Errorf("watch %q: %w", path, watchErr)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk directory %q: %w", root, err)
	}

	return nil
}

// TryAddDirectory attempts to add a path to the watcher if it is a directory.
// Warnings about directories that cannot be watched are written to warnWriter.
func TryAddDirectory(watcher *fsnotify.Watcher, path string, warnWriter io.Writer) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}

	if info.IsDir() {
		addErr := AddRecursive(watcher, path)
		if addErr != nil {
			_, _ = fmt.Fprintf(
				warnWriter,
				"⚠️  failed to watch new directory %s: %v\n",
				path,
				addErr,
			)
		}
	}
}
