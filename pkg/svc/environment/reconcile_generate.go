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
// would render through exists but is not what generation expects — a directory
// segment that is a symlink or regular file, or a layout file that exists as a
// symlink. DerivePlan classifies such an overlay as Missing (it only counts
// real directories), but writing through the entry would follow the link and
// escape the source tree, so generation refuses instead.
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
// Every path is containment-checked before writing: a clusters/ or
// clusters/<env>/ segment that exists as a symlink or regular file — which
// DerivePlan reports as Missing but a write would follow out of the source
// tree — is rejected with [ErrUnsafeOverlayPath], as is a layout file that
// exists as a symlink (a dangling one would otherwise be written through).
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

	for _, file := range files {
		err := ensureSafeOverlayPath(sourceDir, file.RelPath)
		if err != nil {
			return nil, err
		}
	}

	return WriteMultiClusterLayout(gen, sourceDir, files, false)
}

// ensureSafeOverlayPath walks relPath's segments under sourceDir with Lstat:
// every directory segment that already exists must be a real directory (not a
// symlink or file), and the leaf — preserved by the force-off writer when
// present — must not be a symlink (a dangling one would be written through).
// A segment that does not exist ends the walk: everything below it is created
// fresh by the writer.
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
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: %s is a symlink", ErrUnsafeOverlayPath, current)
			}

			return nil
		}

		if !info.IsDir() {
			return fmt.Errorf("%w: %s is not a directory", ErrUnsafeOverlayPath, current)
		}
	}

	return nil
}
