package environment

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// ErrDerivePlan is returned by DerivePlan when the clusters/ overlay tree exists
// but cannot be read (a missing clusters/ directory is a valid pre-scaffold state,
// not an error, and an environment-discovery failure surfaces as
// ErrDiscoverEnvironments).
var ErrDerivePlan = errors.New("failed to derive environment reconcile plan")

// OverlayState classifies a declared environment's clusters/<env>/ overlay in a
// reconcile plan.
type OverlayState string

const (
	// OverlayPresent means the environment's clusters/<env>/ overlay directory
	// exists. This increment only checks existence; content drift against the
	// declared config is a later reconcile increment.
	OverlayPresent OverlayState = "Present"
	// OverlayMissing means the environment declares a ksail.<env>.yaml root config
	// but has no clusters/<env>/ overlay directory — the state a reconcile would
	// resolve by generating the overlay (via the DeriveMultiClusterLayout /
	// CloneOverlay seams).
	OverlayMissing OverlayState = "Missing"
)

// PlanEntry pairs one declared environment with the state of its cluster overlay.
type PlanEntry struct {
	// Environment is the declared environment (from DeriveEnvironments).
	Environment Environment
	// OverlayDir is the environment's overlay directory relative to the GitOps
	// source directory, slash-delimited (clusters/<env>).
	OverlayDir string
	// State reports whether that overlay directory exists.
	State OverlayState
}

// Plan is the read-side reconcile plan for a workspace's declared environments
// (issue #5441 item 3): which declared environments have their clusters/<env>/
// overlay and which are missing it, plus the overlay directories nothing
// declares. It derives purely from the filesystem — no mutation; generation and
// CLI surfacing are follow-up increments that consume this model.
type Plan struct {
	// Entries holds one entry per declared environment, sorted by name.
	Entries []PlanEntry
	// Orphans lists overlay directories (relative to the source directory,
	// slash-delimited) that no ksail.<env>.yaml declares, sorted; the shared
	// clusters/base/ overlay is not an orphan. Orphans are surfaced for the
	// operator to resolve — a reconcile must never delete them silently.
	Orphans []string
}

// DerivePlan diffs the environments declared by ksail.<env>.yaml root configs in
// repoRoot against the overlay tree at <repoRoot>/<sourceDir>/clusters/. A
// missing clusters/ directory is a valid pre-scaffold state: every declared
// environment is then OverlayMissing and there are no orphans.
func DerivePlan(repoRoot, sourceDir string, load ConfigLoader) (Plan, error) {
	declared, err := DeriveEnvironments(repoRoot, load)
	if err != nil {
		return Plan{}, err
	}

	overlays, err := listOverlayDirs(filepath.Join(repoRoot, sourceDir, ClustersDir))
	if err != nil {
		return Plan{}, err
	}

	entries := make([]PlanEntry, 0, len(declared))
	declaredNames := make(map[string]struct{}, len(declared))

	for _, env := range declared {
		declaredNames[env.Name] = struct{}{}

		state := OverlayMissing
		if _, ok := overlays[env.Name]; ok {
			state = OverlayPresent
		}

		entries = append(entries, PlanEntry{
			Environment: env,
			OverlayDir:  path.Join(ClustersDir, env.Name),
			State:       state,
		})
	}

	orphans := make([]string, 0, len(overlays))

	for name := range overlays {
		if name == BaseEnvName {
			continue
		}

		if _, ok := declaredNames[name]; !ok {
			orphans = append(orphans, path.Join(ClustersDir, name))
		}
	}

	slices.SortFunc(orphans, strings.Compare)

	return Plan{Entries: entries, Orphans: orphans}, nil
}

// listOverlayDirs enumerates the overlay directory names under clustersAbs. A
// non-existent clusters/ tree yields an empty set (the pre-scaffold state); any
// other read failure is surfaced as ErrDerivePlan.
func listOverlayDirs(clustersAbs string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(clustersAbs)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]struct{}{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDerivePlan, err)
	}

	overlays := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			overlays[entry.Name()] = struct{}{}
		}
	}

	return overlays, nil
}
