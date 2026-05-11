package fsutil

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
)

// Path expansion operations.

// homeDir caches the current user's home directory after the first successful
// lookup. user.Current() is a syscall; caching it avoids repeated OS
// round-trips in hot paths such as cluster provisioning and kubeconfig
// resolution.
//
// Only the successful result is cached. A transient error (e.g. temporary NSS
// failure) does not poison the cache — the next call retries the syscall.
//
//nolint:gochecknoglobals // sync cache must be package-scoped.
var (
	homeDirMu    sync.Mutex
	homeDirValue string
	homeDirSet   bool
)

// currentHomeDir returns the cached home directory, calling user.Current()
// only when no successful result has been cached yet.
func currentHomeDir() (string, error) {
	homeDirMu.Lock()
	defer homeDirMu.Unlock()

	if homeDirSet {
		return homeDirValue, nil
	}

	usr, err := user.Current()
	if err != nil {
		return "", err
	}

	homeDirValue = usr.HomeDir
	homeDirSet = true

	return homeDirValue, nil
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
