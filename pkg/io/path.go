package io

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strings"
)

// Path expansion operations.

// ExpandHomePath expands a path beginning with ~/ to the user's home directory.
//
// Parameters:
//   - path: The path to expand (e.g., "~/config.yaml")
//
// Returns:
//   - string: The expanded path with home directory substituted
//   - error: Error if unable to get current user information
func ExpandHomePath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		usr, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("failed to get current user: %w", err)
		}

		return filepath.Join(usr.HomeDir, path[2:]), nil
	}

	return path, nil
}
