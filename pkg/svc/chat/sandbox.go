package chat

import (
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

// IsPathWithinDirectory reports whether the given path resolves to a location
// within allowedRoot or is exactly allowedRoot. Both paths are canonicalized
// (absolute + symlinks resolved) before comparison to prevent traversal via
// ".." or symlinks that escape the root.
//
// It delegates to fsutil.IsPathWithinDirectory, the single home for the
// symlink-escape guard, so this sandbox check cannot drift from the other
// callers of the shared helper.
func IsPathWithinDirectory(path, allowedRoot string) bool {
	return fsutil.IsPathWithinDirectory(path, allowedRoot)
}
