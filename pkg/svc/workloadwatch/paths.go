package workloadwatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
)

// FormatElapsed formats a duration as a compact human-readable string
// (e.g. "0.3s", "1.2s", "45.0s").
func FormatElapsed(elapsed time.Duration) string {
	return fmt.Sprintf("%.1fs", elapsed.Seconds())
}

// FindKustomizationDir walks up from the changed path to find the nearest
// directory satisfying the hasKustomization predicate (i.e. containing a
// kustomization file recognized by kubectl). Both changedFile and rootDir are
// normalized to absolute paths before comparison so that mixed relative /
// absolute inputs are handled correctly. If the nearest match is the root watch
// directory or no match is found, rootDir is returned (triggering a full
// reconcile). When changedFile is itself a directory the search starts there
// instead of at its parent.
func FindKustomizationDir(changedFile, rootDir string, hasKustomization func(string) bool) string {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return rootDir
	}

	absChanged, err := filepath.Abs(changedFile)
	if err != nil {
		return absRoot
	}

	// When the changed path is a directory, start the search there;
	// otherwise start at its parent directory.
	dir := filepath.Dir(absChanged)

	info, statErr := os.Stat(absChanged)
	if statErr == nil && info.IsDir() {
		dir = absChanged
	}

	for {
		if hasKustomization(dir) {
			return dir
		}

		// Reached the root watch directory without finding a nested kustomization.
		if dir == absRoot {
			return absRoot
		}

		parent := filepath.Dir(dir)

		// Reached the filesystem root without finding anything.
		if parent == dir {
			return absRoot
		}

		dir = parent
	}
}

// MatchFluxKustomizations maps a changed directory (absolute path) to the
// Flux Kustomization CR(s) whose spec.path matches. A match occurs when
// the normalized relative path of the changed directory equals or is a
// parent/child of the CR's spec.path. Returns nil when no CRs match.
func MatchFluxKustomizations(
	changedDir, rootDir string,
	kustomizations []flux.KustomizationInfo,
) []string {
	relDir, err := filepath.Rel(rootDir, changedDir)
	if err != nil {
		return nil
	}

	relDir = NormalizeFluxPath(relDir)
	if relDir == "" {
		return nil
	}

	var matches []string

	for _, kustomization := range kustomizations {
		ksPath := NormalizeFluxPath(kustomization.Path)
		if ksPath == "" {
			continue
		}

		if ksPath == relDir ||
			strings.HasPrefix(ksPath, relDir+"/") ||
			strings.HasPrefix(relDir, ksPath+"/") {
			matches = append(matches, kustomization.Name)
		}
	}

	return matches
}

// NormalizeFluxPath strips leading "./" and cleans the path, converting
// OS-specific separators to forward slashes so prefix checks work
// consistently across platforms. Returns "" for paths that resolve to "."
// (root-level).
func NormalizeFluxPath(path string) string {
	path = strings.TrimPrefix(path, "./")
	path = filepath.ToSlash(filepath.Clean(path))

	if path == "." {
		return ""
	}

	return path
}
