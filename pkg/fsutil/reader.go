package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// File reading operations.

// ReadFileSafe reads the file at filePath only if it is located within basePath.
// It resolves absolute paths and rejects reads where the resolved path is
// outside basePath (prevents path traversal and accidental file inclusion).
//
// Parameters:
//   - basePath: The base directory that filePath must be within
//   - filePath: The file path to read (must be within basePath)
//
// Returns:
//   - []byte: The file contents
//   - error: ErrPathOutsideBase if path is outside base, or read error
func ReadFileSafe(basePath, filePath string) ([]byte, error) {
	// Canonicalize both paths (absolute + symlinks resolved) before the containment
	// check so that directory-escape via ".." components or symlinks is rejected.
	canonBase, err := EvalCanonicalPath(basePath)
	if err != nil {
		return nil, ErrPathOutsideBase
	}

	canonFile, err := EvalCanonicalPath(filePath)
	if err != nil {
		return nil, ErrPathOutsideBase
	}

	// Use filepath.Rel to enforce directory boundaries. strings.HasPrefix alone is
	// insufficient: "/base_evil/x" would pass a HasPrefix check for "/base".
	rel, err := filepath.Rel(canonBase, canonFile)
	if err != nil {
		return nil, ErrPathOutsideBase
	}

	// Reject paths that resolve outside the base directory by checking whether
	// the first path element is "..". This is stricter than a raw HasPrefix("..")
	// check, which would incorrectly reject valid in-base paths like "..evil/secret".
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, ErrPathOutsideBase
	}

	data, err := os.ReadFile(canonFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", filePath, err)
	}

	return data, nil
}

// EvalCanonicalPath returns the absolute, symlink-resolved form of a path.
// If the path itself does not exist, it resolves the parent directory's symlinks
// and appends the final component, so that containment checks remain accurate for
// paths that are about to be created or have not yet been written.
// It returns an error if the path cannot be made absolute or if symlinks in the
// parent directory cannot be resolved (e.g., due to a missing parent or permissions).
func EvalCanonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolving symlinks: %w", err)
		}

		// Path doesn't exist yet: resolve the parent directory's symlinks and
		// append the final component so the check remains accurate.
		dir := filepath.Dir(abs)
		base := filepath.Base(abs)

		resolvedDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			return "", fmt.Errorf("resolving symlinks for parent: %w", dirErr)
		}

		return filepath.Join(resolvedDir, base), nil
	}

	return resolved, nil
}

// Path resolution operations.

// FindFile resolves a file path with directory traversal.
// For absolute paths, returns the path as-is.
// For relative paths, traverses up from the current directory to find the file.
//
// Parameters:
//   - filePath: The file path to resolve
//
// Returns:
//   - string: The resolved absolute path if found, or the original path if not found
//   - error: Error if unable to get current directory
func FindFile(filePath string) (string, error) {
	// If absolute path, return as-is
	if filepath.IsAbs(filePath) {
		return filePath, nil
	}

	// For relative paths, start from current directory and traverse up
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Traverse up the directory tree looking for the file
	for {
		candidatePath := filepath.Join(currentDir, filePath)

		_, err := os.Stat(candidatePath)
		if err == nil {
			return filepath.Clean(candidatePath), nil
		}

		// Move up one directory
		parentDir := filepath.Dir(currentDir)
		// Stop if we've reached the root directory
		if parentDir == currentDir {
			break
		}

		currentDir = parentDir
	}

	// If not found during traversal, return the original relative path
	// This allows the caller to handle the file-not-found case appropriately
	return filePath, nil
}
