package fsutil

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strings"
)

// Path expansion operations.

// ExpandHomePath expands a path beginning with ~/ to the user's home directory
// and converts relative paths to absolute paths.
//
// Parameters:
//   - path: The path to expand (e.g., "~/config.yaml", "./config.yaml", or "/absolute/path")
//
// Returns:
//   - string: The expanded and absolute path
//   - error: Error if unable to get current user information or convert to absolute path
func ExpandHomePath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		usr, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("failed to get current user: %w", err)
		}

		path = filepath.Join(usr.HomeDir, path[2:])
	}

	// Convert relative paths to absolute paths
	if !filepath.IsAbs(path) {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("failed to convert to absolute path: %w", err)
		}

		return absPath, nil
	}

	return path, nil
}
