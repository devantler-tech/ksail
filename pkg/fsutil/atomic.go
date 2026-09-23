package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var errShortWrite = errors.New("short write")

// AtomicWriteFile writes data to a temp file in the same directory and
// renames it to the target path, ensuring an all-or-nothing write.
//
// os.Rename replaces an existing regular file on every supported platform
// (on Windows it uses MoveFileEx with MOVEFILE_REPLACE_EXISTING), so a failed
// rename is returned unchanged and never retried: the previous destination is
// left intact and only the staging temp file is removed.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return atomicWriteFile(path, data, perm, os.Rename)
}

// atomicWriteFile implements AtomicWriteFile with an injectable rename so
// tests can simulate a rename failure on any platform.
func atomicWriteFile(
	path string,
	data []byte,
	perm os.FileMode,
	rename func(oldpath, newpath string) error,
) error {
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

	bytesWritten, writeErr := tmp.Write(data)
	if writeErr != nil {
		_ = tmp.Close()

		return fmt.Errorf("write data: %w", writeErr)
	}

	if bytesWritten != len(data) {
		_ = tmp.Close()

		return fmt.Errorf(
			"write data: %w: wrote %d of %d bytes",
			errShortWrite,
			bytesWritten,
			len(data),
		)
	}

	closeErr := tmp.Close()
	if closeErr != nil {
		return fmt.Errorf("close temp file: %w", closeErr)
	}

	renameErr := rename(tmpPath, path)
	if renameErr != nil {
		return fmt.Errorf("rename temp file: %w", renameErr)
	}

	return nil
}
