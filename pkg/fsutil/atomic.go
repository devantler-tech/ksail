package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// AtomicWriteFile writes data to a temp file in the same directory and
// renames it to the target path, ensuring an all-or-nothing write.
// On Windows, if the rename fails because the destination exists, it
// removes the target and retries once.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".atomic-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tmpPath := tmp.Name()

	defer func() {
		_ = os.Remove(tmpPath)
	}()

	chmodErr := os.Chmod(tmpPath, perm)
	if chmodErr != nil {
		_ = tmp.Close()

		return fmt.Errorf("set permissions: %w", chmodErr)
	}

	_, writeErr := tmp.Write(data)
	if writeErr != nil {
		_ = tmp.Close()

		return fmt.Errorf("write data: %w", writeErr)
	}

	closeErr := tmp.Close()
	if closeErr != nil {
		return fmt.Errorf("close temp file: %w", closeErr)
	}

	renameErr := os.Rename(tmpPath, path)
	if renameErr != nil && runtime.GOOS == "windows" {
		_, statErr := os.Stat(path)
		if statErr == nil {
			_ = os.Remove(path)

			renameErr = os.Rename(tmpPath, path)
		}
	}

	if renameErr != nil {
		return fmt.Errorf("rename temp file: %w", renameErr)
	}

	return nil
}
