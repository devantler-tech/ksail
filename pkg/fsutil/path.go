package fsutil

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
)

// Path expansion operations.

// homeDir caches the current user's home directory after the first lookup.
// user.Current() is a syscall; caching it avoids repeated OS round-trips in
// hot paths such as cluster provisioning and kubeconfig resolution.
//
//nolint:gochecknoglobals // sync.Once cache must be package-scoped.
var (
	homeDirOnce  sync.Once
	homeDirValue string
	errHomeDir   error
)

// currentHomeDir returns the cached home directory, calling user.Current() at
// most once per process lifetime.
func currentHomeDir() (string, error) {
	homeDirOnce.Do(func() {
		usr, err := user.Current()
		if err != nil {
			errHomeDir = err

			return
		}

		homeDirValue = usr.HomeDir
	})

	return homeDirValue, errHomeDir
}

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
		homeDir, err := currentHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get current user: %w", err)
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
