package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Path expansion operations.

// ExpandHomePath expands a path beginning with ~/ to the user's home directory
// and converts relative paths to absolute paths.
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
	if strings.HasPrefix(path, "~/") {
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

// ListYAMLFiles returns the paths of .yaml/.yml files directly within dir
// (non-recursive), skipping subdirectories. The dir argument is used as given —
// callers are responsible for any canonicalization (e.g. EvalCanonicalPath or
// filepath.Clean) before calling. Returned paths are dir joined with each entry name.
func ListYAMLFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dir, err)
	}

	var paths []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		paths = append(paths, filepath.Join(dir, name))
	}

	return paths, nil
}
