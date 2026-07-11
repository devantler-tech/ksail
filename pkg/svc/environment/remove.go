package environment

import (
	"errors"
	"fmt"
	"os"
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

	segments := strings.Split(strings.Trim(overlayRelDir, "/"), "/")
	if len(segments) >= 2 && segments[len(segments)-1] == BaseEnvName &&
		segments[len(segments)-2] == ClustersDir {
		return false, fmt.Errorf("%w: %s", ErrSharedBaseOverlay, overlayRelDir)
	}

	abs, err := containedPath(repoRoot, filepath.FromSlash(overlayRelDir))
	if err != nil {
		return false, fmt.Errorf("overlay %s escapes the repository root: %w",
			overlayRelDir, fsutil.ErrPathOutsideBase)
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

	err = os.RemoveAll(abs)
	if err != nil {
		return false, fmt.Errorf("removing overlay %s: %w", overlayRelDir, err)
	}

	return true, nil
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
