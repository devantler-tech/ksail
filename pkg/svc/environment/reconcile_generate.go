package environment

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrUnsafeOverlayPath is returned by GenerateMissingOverlays when a path it
// would render through exists but is not what generation expects — a source
// directory or directory segment that is a symlink or regular file, or a
// layout file that exists as anything but a regular file (a symlink would be
// written through; a directory makes the force-off writer skip the file while
// reporting it, leaving a silently broken layout). DerivePlan classifies such
// an overlay as Missing (it only counts real directories), but writing
// through the entry would follow the link and escape the source tree, so
// generation refuses instead.
var ErrUnsafeOverlayPath = errors.New(
	"refusing to generate through a non-directory overlay path",
)

// GenerateMissingOverlays scaffolds the clusters/<env>/ overlay for every
// [OverlayMissing] entry in plan, composing the seams the multi-cluster
// scaffold already uses: each missing environment's files come from
// [DeriveMultiClusterLayout] (the shared clusters/base/ plus the overlay
// referencing it), the shared base is deduplicated across entries, and
// rendering goes through [WriteMultiClusterLayout] with force hardwired off —
// generation never overwrites an existing file (a populated clusters/base/ is
// preserved by the writer's idempotent semantics) and never touches orphan
// overlays (the plan surfaces them for the operator; a reconcile must never
// delete them). It returns the resolved output paths in layout order; per the
// writer's contract an existing, untouched file is still reported, so a
// second run reports the same paths while rewriting nothing.
//
// Every path is containment-checked before writing: sourceDir's existing
// ancestors are first canonicalized (so the checks and the writes agree on
// one physical tree — a symlinked ancestor like macOS's /var is resolved up
// front, never followed blindly at write time), then sourceDir itself and any
// clusters/ or clusters/<env>/ segment that exists as a symlink or regular
// file — which DerivePlan reports as Missing but a write would follow out of
// the source tree — is rejected with [ErrUnsafeOverlayPath], as is a layout
// file that exists as anything but a regular file (a dangling symlink would
// be written through; a directory would make the force-off writer skip the
// file while still reporting it as resolved).
func GenerateMissingOverlays(
	gen KustomizationGenerator,
	sourceDir string,
	plan Plan,
) ([]string, error) {
	files := make([]LayoutFile, 0, len(plan.Entries)+1)
	seen := make(map[string]struct{}, len(plan.Entries)+1)

	for _, entry := range plan.Entries {
		if entry.State != OverlayMissing {
			continue
		}

		layout, err := DeriveMultiClusterLayout(entry.Environment.Name)
		if err != nil {
			return nil, fmt.Errorf(
				"generate overlay for environment %q: %w", entry.Environment.Name, err,
			)
		}

		for _, file := range layout {
			if _, dup := seen[file.RelPath]; dup {
				continue
			}

			seen[file.RelPath] = struct{}{}

			files = append(files, file)
		}
	}

	if len(files) > 0 {
		resolved, err := prepareSourceDir(sourceDir)
		if err != nil {
			return nil, err
		}

		sourceDir = resolved
	}

	for _, file := range files {
		err := ensureSafeOverlayPath(sourceDir, file.RelPath)
		if err != nil {
			return nil, err
		}
	}

	return WriteMultiClusterLayout(gen, sourceDir, files, false)
}

// prepareSourceDir canonicalizes sourceDir's existing ancestors and then
// fail-closes on an unsafe source root, returning the physical path every
// guard and write below operates on.
func prepareSourceDir(sourceDir string) (string, error) {
	resolved, err := resolveSourceDirAncestors(sourceDir)
	if err != nil {
		return "", err
	}

	err = ensureSafeSourceDir(resolved)
	if err != nil {
		return "", err
	}

	return resolved, nil
}

// resolveSourceDirAncestors canonicalizes every EXISTING ancestor of
// sourceDir with [filepath.EvalSymlinks] (macOS `/var` → `/private/var` and
// the like), leaving the leaf untouched so ensureSafeSourceDir still rejects
// a symlinked source root itself. The guards and the writer then agree on
// one physical path: an ancestor symlink is resolved to its canonical target
// up front instead of being silently followed at write time, so every
// Lstat-based check below the root inspects the same tree the writes land
// in. Ancestors that do not exist yet are kept verbatim — the writer creates
// them fresh.
func resolveSourceDirAncestors(sourceDir string) (string, error) {
	parent := filepath.Dir(sourceDir)
	if parent == sourceDir {
		// Filesystem root (or a bare "." / ".."): nothing left to resolve.
		return sourceDir, nil
	}

	base := filepath.Base(sourceDir)

	resolved, err := filepath.EvalSymlinks(parent)
	if errors.Is(err, os.ErrNotExist) {
		resolvedParent, parentErr := resolveSourceDirAncestors(parent)
		if parentErr != nil {
			return "", parentErr
		}

		return filepath.Join(resolvedParent, base), nil
	}

	if err != nil {
		return "", fmt.Errorf("resolve source directory ancestors of %q: %w", sourceDir, err)
	}

	return filepath.Join(resolved, base), nil
}

// ensureSafeSourceDir rejects a sourceDir that exists as anything but a real
// directory. The per-file walk starts below sourceDir, so a symlinked source
// root would never be inspected there while every write still goes through
// it into the link target. A sourceDir that does not exist yet is fine — the
// writer creates it fresh.
func ensureSafeSourceDir(sourceDir string) error {
	info, err := os.Lstat(sourceDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect source directory %q: %w", sourceDir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrUnsafeOverlayPath, sourceDir)
	}

	return nil
}

// ensureSafeOverlayPath walks relPath's segments under sourceDir with Lstat:
// every directory segment that already exists must be a real directory (not a
// symlink or file), and the leaf — preserved by the force-off writer when
// present — must be a regular file (a dangling symlink would be written
// through; a directory or other non-regular node would make the writer skip
// the file while reporting it as resolved). A segment that does not exist
// ends the walk: everything below it is created fresh by the writer.
func ensureSafeOverlayPath(sourceDir, relPath string) error {
	current := sourceDir
	segments := strings.Split(path.Clean(relPath), "/")

	for index, segment := range segments {
		current = filepath.Join(current, segment)

		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("inspect overlay path %q: %w", current, err)
		}

		if index == len(segments)-1 {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%w: %s is not a regular file", ErrUnsafeOverlayPath, current)
			}

			return nil
		}

		if !info.IsDir() {
			return fmt.Errorf("%w: %s is not a directory", ErrUnsafeOverlayPath, current)
		}
	}

	return nil
}
