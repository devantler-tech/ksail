package environment

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

// ErrEnvironmentConfigMissing is returned by RemoveEnvironmentConfig when
// repoRoot/configRel does not exist or is not a regular file.
var ErrEnvironmentConfigMissing = errors.New("environment config not found")

// ErrSharedBaseOverlay is returned by RemoveOverlay when the overlay to delete
// is the shared base overlay (clusters/base), which every environment builds
// on and which no single environment owns.
var ErrSharedBaseOverlay = errors.New("refusing to delete the shared base overlay")

// ErrRootEquivalentOverlay is returned by RemoveOverlay when the overlay path
// cleans to the repository root itself (empty, ".", "/", "clusters/..", …):
// containedPath accepts the root as "contained", so without this refusal the
// recursive delete behind `env rm --purge` would remove the entire workspace
// instead of one environment's overlay.
var ErrRootEquivalentOverlay = errors.New("refusing to delete a root-equivalent overlay path")

// ErrNonDirectoryOverlay is returned by RemoveOverlay when the overlay path
// exists but is a regular file (or other non-directory): an overlay is by
// definition a directory, so a non-directory there marks a malformed
// workspace that a purge must surface, not silently delete.
var ErrNonDirectoryOverlay = errors.New("refusing to delete a non-directory overlay path")

// RemoveEnvironmentConfig deletes a single root environment config file (e.g.
// ksail.<name>.yaml) under repoRoot. The path is containment-checked against
// repoRoot — a configRel with ".." segments or a symlink escape is rejected
// rather than deleting outside the repository — and a missing or non-regular
// target reports ErrEnvironmentConfigMissing so the caller can enrich the
// error with the environments that are actually declared.
func RemoveEnvironmentConfig(repoRoot, configRel string) error {
	abs, err := containedPath(repoRoot, configRel)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrEnvironmentConfigMissing, configRel)
	}

	info, err := os.Lstat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrEnvironmentConfigMissing, configRel)
	}

	err = os.Remove(abs)
	if err != nil {
		return fmt.Errorf("removing %s: %w", configRel, err)
	}

	return nil
}

// RemoveOverlay deletes an environment's overlay directory (e.g.
// <sourceDir>/clusters/<name>) under repoRoot, returning whether a directory
// was actually removed (false when the overlay does not exist — a declared
// environment without an overlay is legal, so its absence is not an error).
// The path is containment-checked against repoRoot, a symlinked overlay only
// has its link removed (never the target), and the shared base overlay
// (clusters/[BaseEnvName]) is always refused because every environment's
// kustomization builds on it.
func RemoveOverlay(repoRoot, overlayRelDir string) (bool, error) {
	overlayRelDir = filepath.ToSlash(overlayRelDir)

	abs, err := guardedOverlayPath(repoRoot, overlayRelDir)
	if err != nil {
		return false, err
	}

	info, err := os.Lstat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("checking overlay %s: %w", overlayRelDir, err)
	}

	// A symlinked overlay is removed as a link only: os.Remove drops the link
	// itself and never follows it, so a link pointing outside the repository
	// cannot cause an out-of-tree recursive delete.
	if info.Mode()&os.ModeSymlink != 0 {
		err = os.Remove(abs)
		if err != nil {
			return false, fmt.Errorf("removing overlay symlink %s: %w", overlayRelDir, err)
		}

		return true, nil
	}

	// An overlay is by definition a directory; a regular file at the overlay
	// path is a malformed workspace, not something to recursively delete.
	if !info.IsDir() {
		return false, fmt.Errorf("%w: %s", ErrNonDirectoryOverlay, overlayRelDir)
	}

	canonAbs, err := canonicalContainedOverlay(repoRoot, abs, overlayRelDir)
	if err != nil {
		return false, err
	}

	err = os.RemoveAll(canonAbs)
	if err != nil {
		return false, fmt.Errorf("removing overlay %s: %w", overlayRelDir, err)
	}

	return true, nil
}

// guardedOverlayPath runs RemoveOverlay's lexical pre-delete guards over a
// slash-form overlay path and returns the contained absolute path: a
// root-equivalent path (one that cleans to the repository root — empty, ".",
// "/", "clusters/..") is refused because containedPath accepts the root as
// contained, the shared base overlay is refused because every environment
// builds on it, and an escaping path is refused by the containment check.
func guardedOverlayPath(repoRoot, overlayRelDir string) (string, error) {
	if cleaned := path.Clean(overlayRelDir); cleaned == "." || cleaned == "/" {
		return "", fmt.Errorf("%w: %q", ErrRootEquivalentOverlay, overlayRelDir)
	}

	if isSharedBaseOverlay(overlayRelDir) {
		return "", fmt.Errorf("%w: %s", ErrSharedBaseOverlay, overlayRelDir)
	}

	abs, err := containedPath(repoRoot, filepath.FromSlash(overlayRelDir))
	if err != nil {
		return "", fmt.Errorf("overlay %s escapes the repository root: %w",
			overlayRelDir, fsutil.ErrPathOutsideBase)
	}

	return abs, nil
}

// canonicalContainedOverlay re-verifies the overlay's containment on the
// symlink-RESOLVED path before a recursive delete: containedPath is purely
// lexical, so an intermediate symlinked segment (e.g. k8s/clusters pointing at
// a shared checkout outside the repository) passes it while os.RemoveAll would
// traverse the link and recursively delete the outside target. The resolved
// path must still sit under the resolved repository root and must not resolve
// onto the shared base overlay (a parent link could alias another name onto
// clusters/base, sidestepping the lexical base refusal).
func canonicalContainedOverlay(repoRoot, abs, overlayRelDir string) (string, error) {
	canonRoot, err := fsutil.EvalCanonicalPath(repoRoot)
	if err != nil {
		return "", fmt.Errorf("resolving repository root: %w", err)
	}

	canonAbs, err := fsutil.EvalCanonicalPath(abs)
	if err != nil {
		return "", fmt.Errorf("resolving overlay %s: %w", overlayRelDir, err)
	}

	rel, err := filepath.Rel(canonRoot, canonAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("overlay %s escapes the repository root: %w",
			overlayRelDir, fsutil.ErrPathOutsideBase)
	}

	// The lexical root-equivalence guard ran on the UNRESOLVED path; an
	// intermediate symlink can still collapse the RESOLVED overlay onto the
	// repository root itself (e.g. clusters -> the workspace root plus an
	// environment named like the checkout), which would make the recursive
	// delete below wipe the whole repository.
	if rel == "." {
		return "", fmt.Errorf("%w: %q", ErrRootEquivalentOverlay, overlayRelDir)
	}

	if isSharedBaseOverlay(rel) {
		return "", fmt.Errorf("%w: %s", ErrSharedBaseOverlay, overlayRelDir)
	}

	return canonAbs, nil
}

// isSharedBaseOverlay reports whether the slash- or OS-delimited relative path
// ends in the shared base overlay (clusters/[BaseEnvName]), which no single
// environment owns and RemoveOverlay always refuses to delete.
func isSharedBaseOverlay(rel string) bool {
	segments := strings.Split(strings.Trim(filepath.ToSlash(rel), "/"), "/")

	return len(segments) >= 2 && segments[len(segments)-1] == BaseEnvName &&
		segments[len(segments)-2] == ClustersDir
}

// containedPath joins relPath onto repoRoot and rejects a result that escapes
// repoRoot lexically (filepath.Join collapses ".." segments, so an escaping
// input yields a relative path starting with ".."), mirroring writeClone's
// containment guard.
func containedPath(repoRoot, relPath string) (string, error) {
	abs := filepath.Join(repoRoot, relPath)

	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fsutil.ErrPathOutsideBase
	}

	return abs, nil
}
