package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Writer operations.

// File writing operations.

// TryWriteFile writes content to a file path, handling force/overwrite logic.
// It validates that the output path doesn't contain path traversal attempts.
//
// Parameters:
//   - content: The content to write to the file
//   - output: The output file path
//   - force: If true, overwrites existing files; if false, skips existing files
//
// Returns:
//   - string: The content that was written (for chaining)
//   - error: ErrEmptyOutputPath if output is empty, or write error
//
// Caller responsibilities:
//   - Ensure the output path is within intended bounds
//   - Handle the returned content appropriately
func TryWriteFile(content string, output string, force bool) (string, error) {
	if output == "" {
		return "", ErrEmptyOutputPath
	}

	// Clean the output path
	output = filepath.Clean(output)

	// Check if file exists and we're not forcing
	if !force {
		_, err := os.Stat(output)
		if err == nil {
			return content, nil // File exists and force is false, skip writing
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("failed to check file %s: %w", output, err)
		}
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(output)

	err := os.MkdirAll(dir, dirPermUserGroupRX)
	if err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write the file using os.WriteFile
	err = os.WriteFile(output, []byte(content), filePermUserRW)
	if err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", output, err)
	}

	return content, nil
}

// TryWriteFileWithin writes content to relPath interpreted relative to basePath,
// returning whether a file was actually written.
//
// Unlike [TryWriteFile], which takes an already-joined path and resolves it by
// name, every operation here goes through an [os.Root] handle on basePath. os.Root
// resolves each path component against a retained directory descriptor, so a
// directory that is replaced by a symlink after a caller's containment check
// cannot redirect the write outside basePath. That closes the window between
// checking a destination and writing it, which a name-based write leaves open.
//
// Symlinks that stay within basePath are still followed; callers that must reject
// those too should keep their own lstat-based policy check. The escape guarantee
// here does not depend on that check winning a race.
//
// Parameters:
//   - basePath: The directory the write is confined to
//   - relPath: The destination path relative to basePath (slash- or OS-separated)
//   - content: The content to write
//   - force: If true, truncates an existing file; if false, leaves it untouched
//
// Returns:
//   - bool: true if a file was written, false if an existing file was preserved
//   - error: ErrBasePath, ErrEmptyOutputPath, ErrPathOutsideBase, or a write error
func TryWriteFileWithin(basePath, relPath, content string, force bool) (bool, error) {
	rel, err := cleanRelPathWithin(basePath, relPath)
	if err != nil {
		return false, err
	}

	root, err := os.OpenRoot(basePath)
	if err != nil {
		return false, fmt.Errorf("failed to open base directory %s: %w", basePath, err)
	}

	defer func() { _ = root.Close() }()

	if dir := filepath.Dir(rel); dir != "." {
		err = root.MkdirAll(dir, dirPermUserGroupRX)
		if err != nil {
			return false, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return writeThroughRoot(root, rel, relPath, content, force)
}

// cleanRelPathWithin normalises relPath and rejects anything that is not a
// relative path descending from basePath. os.Root would refuse an escape anyway;
// rejecting here returns the package's own sentinel errors instead of a wrapped
// syscall error.
func cleanRelPathWithin(basePath, relPath string) (string, error) {
	if basePath == "" {
		return "", ErrBasePath
	}

	if relPath == "" {
		return "", ErrEmptyOutputPath
	}

	rel := filepath.Clean(filepath.FromSlash(relPath))
	if filepath.IsAbs(rel) || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideBase
	}

	return rel, nil
}

// writeThroughRoot creates or replaces rel inside root, reporting whether it
// wrote. displayPath is the caller's original spelling, used only in errors.
func writeThroughRoot(
	root *os.Root, rel, displayPath, content string, force bool,
) (bool, error) {
	// O_EXCL makes the "skip if it already exists" decision atomic with the create,
	// so the non-force path has no stat-then-write window either.
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}

	file, err := root.OpenFile(rel, flags, filePermUserRW)
	if err != nil {
		if !force && errors.Is(err, os.ErrExist) {
			return false, nil
		}

		return false, fmt.Errorf("failed to write file %s: %w", displayPath, err)
	}

	_, err = file.WriteString(content)
	if err != nil {
		_ = file.Close()

		return false, fmt.Errorf("failed to write file %s: %w", displayPath, err)
	}

	err = file.Close()
	if err != nil {
		return false, fmt.Errorf("failed to write file %s: %w", displayPath, err)
	}

	return true, nil
}
