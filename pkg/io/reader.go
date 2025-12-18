package io

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
	filePath = filepath.Clean(filePath)

	if !strings.HasPrefix(filePath, basePath) {
		return nil, ErrPathOutsideBase
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", filePath, err)
	}

	return data, nil
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
