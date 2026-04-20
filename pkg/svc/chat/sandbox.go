package chat

import (
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

// IsPathWithinDirectory reports whether the given path resolves to a location
// within allowedRoot or is exactly allowedRoot. Both paths are canonicalized
// (absolute + symlinks resolved) before comparison to prevent traversal via
// ".." or symlinks that escape the root.
func IsPathWithinDirectory(path, allowedRoot string) bool {
	if path == "" || allowedRoot == "" {
		return false
	}

	resolvedRoot, err := fsutil.EvalCanonicalPath(allowedRoot)
	if err != nil {
		return false
	}

	resolvedPath, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return false
	}

	if resolvedPath == resolvedRoot {
		return true
	}

	return strings.HasPrefix(resolvedPath, resolvedRoot+string(os.PathSeparator))
}
