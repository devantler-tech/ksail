package fsutil

import (
	"path/filepath"
	"strings"
)

// Path-containment operations.

// IsPathWithinDirectory reports whether path resolves to a location within
// allowedRoot, or is exactly allowedRoot. Both arguments are canonicalized
// (made absolute and symlink-resolved via EvalCanonicalPath) before the
// comparison, so traversal via ".." components or symlinks that escape the
// root is rejected. Empty inputs and any canonicalization failure return false.
//
// This is the single home for the symlink-escape guard: callers that need a
// boolean containment check (chat sandboxing, cluster-directory writes) must
// use this rather than re-deriving the filepath.Rel / HasPrefix logic, so the
// guard cannot drift between call sites.
func IsPathWithinDirectory(path, allowedRoot string) bool {
	if path == "" || allowedRoot == "" {
		return false
	}

	canonRoot, err := EvalCanonicalPath(allowedRoot)
	if err != nil {
		return false
	}

	canonPath, err := EvalCanonicalPath(path)
	if err != nil {
		return false
	}

	return canonicalPathWithin(canonRoot, canonPath)
}

// canonicalPathWithin reports whether the already-canonical canonPath is equal
// to or nested under the already-canonical canonRoot. filepath.Rel is used
// rather than a raw string prefix so that a sibling such as "/base_evil/x"
// is not mistaken for a child of "/base".
func canonicalPathWithin(canonRoot, canonPath string) bool {
	if canonPath == canonRoot {
		return true
	}

	rel, err := filepath.Rel(canonRoot, canonPath)
	if err != nil {
		return false
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	return true
}
