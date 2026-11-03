package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// Path expansion operations.

// ExpandHomePath expands ~/ and, on Windows, ~\ to the user's home directory.
// It also converts relative paths to absolute paths.
//
// The home directory is resolved via os.UserHomeDir(), which honors $HOME (or
// %USERPROFILE% on Windows). Resolving through the environment — rather than
// the OS user database (user.Current) — is what every standard tool does, and
// it lets tests redirect home-derived paths (e.g. ~/.kube/config) to a
// temporary directory by overriding $HOME, so the suite never reads from or
// writes to the developer's real configuration.
//
// Parameters:
//   - path: The path to expand (e.g., "~/config.yaml", "./config.yaml", or "/absolute/path")
//
// Returns:
//   - string: The expanded and absolute path
//   - error: Error if unable to get the home directory or convert to absolute path
func ExpandHomePath(path string) (string, error) {
	if len(path) > 1 && path[0] == '~' && os.IsPathSeparator(path[1]) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}

		path = filepath.Join(homeDir, path[2:])
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
