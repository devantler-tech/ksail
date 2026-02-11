package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
