// Package pathutil provides filesystem path canonicalization without tool dependencies.
package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// EvalCanonicalPath returns the absolute, symlink-resolved form of a path.
// If the path does not exist, it resolves its parent and appends the final component.
func EvalCanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolving symlinks: %w", err)
		}

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
