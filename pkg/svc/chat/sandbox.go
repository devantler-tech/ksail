package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsPathWithinDirectory reports whether the given path resolves to a location
// within allowedRoot or is exactly allowedRoot. Both paths are canonicalized
// (absolute + symlinks resolved) before comparison to prevent traversal via
// ".." or symlinks that escape the root.
func IsPathWithinDirectory(path, allowedRoot string) bool {
	if path == "" || allowedRoot == "" {
		return false
	}

	resolvedRoot, err := resolveCanonicalPath(allowedRoot)
	if err != nil {
		return false
	}

	resolvedPath, err := resolveCanonicalPath(path)
	if err != nil {
		return false
	}

	if resolvedPath == resolvedRoot {
		return true
	}

	return strings.HasPrefix(resolvedPath, resolvedRoot+string(os.PathSeparator))
}

// resolveCanonicalPath returns the absolute, symlink-resolved form of a path.
func resolveCanonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolving symlinks: %w", err)
		}

		// Path doesn't exist yet (e.g. a write target): resolve the parent
		// directory and append the final component.
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
