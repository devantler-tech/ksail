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
	// Entries holds one entry per declared environment, sorted by name —
	// including the initial environment the base ksail.yaml declares through
	// its kustomizationFile (which has no ksail.<env>.yaml of its own).
	Entries []PlanEntry
	// Orphans lists overlay directories (relative to the source directory,
	// slash-delimited) that no ksail.<env>.yaml declares, sorted; the shared
	// clusters/base/ overlay is not an orphan. Orphans are surfaced for the
	// operator to resolve — a reconcile must never delete them silently.
	Orphans []string
}

// baseConfigFileName is the workspace's base root config — not itself an
// environment (DeriveEnvironments excludes it), but its kustomizationFile may
// sync a clusters/<env> overlay that DerivePlan must not misread as orphaned.
const baseConfigFileName = "ksail.yaml"

// DerivePlan diffs the environments declared by ksail.<env>.yaml root configs in
// repoRoot against the overlay tree at <sourceDir>/clusters/ (sourceDir is taken
// relative to repoRoot unless already absolute — the ksail config manager hands
// downstream consumers an absolutized source directory). A missing clusters/
// directory is a valid pre-scaffold state: every declared environment is then
// OverlayMissing and there are no orphans.
//
// A declared environment named after the reserved shared base overlay
// (ksail.base.yaml) is rejected with ErrReservedEnvironmentName: clusters/base
// is the shared base every overlay builds on, so treating it as that
// environment's overlay would mask the name collision DeriveMultiClusterLayout
// already refuses. The overlay synced by the base ksail.yaml's
// kustomizationFile (the initial environment `project init --multi-cluster`
// scaffolds without a ksail.<env>.yaml) is recognised and never reported as an
// orphan.
func DerivePlan(repoRoot, sourceDir string, load ConfigLoader) (Plan, error) {
	declared, err := DeriveEnvironments(repoRoot, load)
	if err != nil {
		return Plan{}, err
	}

	clustersDir := filepath.Join(repoRoot, sourceDir, ClustersDir)
	if filepath.IsAbs(sourceDir) {
		clustersDir = filepath.Join(sourceDir, ClustersDir)
	}

	overlays, err := listOverlayDirs(clustersDir)
	if err != nil {
		return Plan{}, err
	}

	entries, declaredNames, err := planEntries(declared, overlays)
	if err != nil {
		return Plan{}, err
	}

	entries, declaredNames = appendBaseSyncedEntry(entries, declaredNames, overlays, load)

	return Plan{
		Entries: entries,
		Orphans: orphanOverlays(overlays, declaredNames),
	}, nil
}

// appendBaseSyncedEntry adds the environment declared by the base ksail.yaml's
// kustomizationFile (the initial environment `project init --multi-cluster`
// scaffolds without a ksail.<env>.yaml) as a first-class plan entry, so a
// deleted clusters/<env> overlay is reported as Missing — and reconciled — like
// any other declared environment, rather than only being kept out of the
// orphan list. A name also declared by its own ksail.<env>.yaml keeps that
// entry (no duplicate), and a base config syncing the shared clusters/base
// overlay declares nothing.
func appendBaseSyncedEntry(
	entries []PlanEntry,
	declaredNames map[string]struct{},
	overlays map[string]struct{},
	load ConfigLoader,
) ([]PlanEntry, map[string]struct{}) {
	baseEnv, ok := baseConfigEnvironment(load)
	if !ok || baseEnv.Name == BaseEnvName {
		return entries, declaredNames
	}

	if _, dup := declaredNames[baseEnv.Name]; dup {
		return entries, declaredNames
	}

	declaredNames[baseEnv.Name] = struct{}{}

	state := OverlayMissing
	if _, exists := overlays[baseEnv.Name]; exists {
		state = OverlayPresent
	}

	entries = append(entries, PlanEntry{
		Environment: baseEnv,
		OverlayDir:  path.Join(ClustersDir, baseEnv.Name),
		State:       state,
	})

	slices.SortFunc(entries, func(left, right PlanEntry) int {
		return strings.Compare(left.Environment.Name, right.Environment.Name)
	})

	return entries, declaredNames
}

// planEntries pairs each declared environment with its overlay state, rejecting
// the reserved base name (see DerivePlan), and returns the declared-name set the
// orphan scan filters against.
func planEntries(
	declared []Environment,
	overlays map[string]struct{},
) ([]PlanEntry, map[string]struct{}, error) {
	entries := make([]PlanEntry, 0, len(declared))
	declaredNames := make(map[string]struct{}, len(declared))

	for _, env := range declared {
		if env.Name == BaseEnvName {
			return nil, nil, fmt.Errorf(
				"%w: %s declares environment %q, which is the shared clusters/%s overlay",
				ErrReservedEnvironmentName, env.ConfigFile, env.Name, BaseEnvName,
			)
		}

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

	return entries, declaredNames, nil
}

// orphanOverlays lists the overlay directories nothing declares, sorted —
// excluding the shared base overlay (declaredNames already includes the
// overlay the base ksail.yaml syncs, via appendBaseSyncedEntry).
func orphanOverlays(
	overlays map[string]struct{},
	declaredNames map[string]struct{},
) []string {
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

	return orphans
}

// baseConfigEnvironment derives the environment the workspace's base
// ksail.yaml declares through its workload kustomizationFile (clusters/<name>),
// reporting ok=false when there is no loadable base config or its sync path is
// not exactly one directory under clusters/. `project init --multi-cluster
// <env>` scaffolds this initial environment without a ksail.<env>.yaml, so
// DerivePlan must learn it from the base config: its overlay is a real plan
// entry (reported Missing when deleted), never an orphan.
func baseConfigEnvironment(load ConfigLoader) (Environment, bool) {
	cfg, err := load(baseConfigFileName)
	if err != nil || cfg == nil {
		return Environment{}, false
	}

	sync := path.Clean(filepath.ToSlash(cfg.Spec.Workload.KustomizationFile))

	dir, name := path.Split(sync)
	if path.Clean(dir) != ClustersDir || name == "" {
		return Environment{}, false
	}

	return Environment{
		Name:         name,
		ConfigFile:   baseConfigFileName,
		Distribution: cfg.Spec.Cluster.Distribution,
		Provider:     cfg.Spec.Cluster.Provider,
	}, true
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
