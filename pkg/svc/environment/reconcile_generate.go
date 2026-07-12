package environment

import (
	"fmt"
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

	return WriteMultiClusterLayout(gen, sourceDir, files, false)
}
