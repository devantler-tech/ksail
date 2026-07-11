package env

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/spf13/cobra"
)

// rmLongDesc documents the removal semantics: the declared root config is what
// an environment *is*, so removing it un-declares the environment; the overlay
// holds user-authored manifests and is only deleted on explicit opt-in.
const rmLongDesc = `Remove a declared cluster environment.

Deletes the environment's root config (ksail.<name>.yaml), which un-declares the
environment. The cluster overlay (<sourceDirectory>/clusters/<name>/) holds
user-authored manifests and is retained by default; pass --purge to delete it in
the same run. The shared base overlay (clusters/base) is never deleted.

Examples:
  # Un-declare the "staging" environment, keeping its overlay
  ksail project env rm staging

  # Remove "staging" including its cluster overlay
  ksail project env rm staging --purge`

// NewRmCmd creates and returns the `project env rm` command.
func NewRmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "rm <name>",
		Aliases:      []string{"remove"},
		Short:        "Remove a declared cluster environment",
		Long:         rmLongDesc,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cmd.Flags().Bool(
		"purge", false,
		"Also delete the environment's cluster overlay (<sourceDirectory>/clusters/<name>)",
	)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return HandleRmRunE(cmd, args[0])
	}

	// env rm is a net-new, state-modifying (file-deleting) command, so it ships
	// behind the experimental gate per the repo's feature-flag-first convention;
	// the visible-command carve-out covers only low-risk read-only additions
	// (like env list). Graduate by dropping this Guard call once the command
	// has settled through a release.
	return experimental.Guard(cmd)
}

// HandleRmRunE handles the `project env rm` command. It resolves the declared
// environment (loading its config the same silent, validation-skipping way
// `env add` resolves --from, so a mistyped name reports what is available),
// removes the root config, and removes or reports the overlay per --purge.
// Exported for testing.
func HandleRmRunE(cmd *cobra.Command, name string) error {
	purge, _ := cmd.Flags().GetBool("purge")

	err := validateEnvironmentName(name)
	if err != nil {
		return fmt.Errorf("invalid environment name: %w", err)
	}

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Canonicalise so the repository root matches the symlink-resolved paths the
	// config loader derives, mirroring `env add`.
	repoRoot, err := fsutil.EvalCanonicalPath(workDir)
	if err != nil {
		return fmt.Errorf("failed to resolve current directory: %w", err)
	}

	configRel := "ksail." + name + ".yaml"

	// Locating the overlay needs the environment's config (for its declared
	// sourceDirectory). Only --purge REQUIRES that resolution — it must know
	// what it is about to delete — so a config that fails to load or resolve
	// blocks a purge but not the default removal, which only deletes the root
	// config and retains the overlay (the retained-overlay hint is then
	// skipped).
	overlayRel, overlayErr := resolveOverlayRel(cmd, repoRoot, name)
	if purge && overlayErr != nil {
		return overlayErr
	}

	return removeEnvironment(cmd, repoRoot, configRel, overlayRel, purge)
}

// resolveOverlayRel loads the environment's config and derives its overlay
// directory (sourceDirectory/clusters/<name>) relative to the repository root,
// enriching a load failure with the environments that are actually declared.
func resolveOverlayRel(cmd *cobra.Command, repoRoot, name string) (string, error) {
	cfg, err := loadEnvironmentConfig(cmd, "ksail."+name+".yaml")
	if err != nil {
		return "", enrichSourceConfigError(cmd, repoRoot, err)
	}

	sourceDir, err := repoRelativeSourceDir(repoRoot, cfg.Spec.Workload.SourceDirectory)
	if err != nil {
		return "", err
	}

	return path.Join(sourceDir, clustersDirSegment, name), nil
}

// removeEnvironment performs the actual removal and reports what happened: the
// config always goes; the overlay goes only with --purge, and is otherwise
// pointed out so the user knows the retained files exist.
//
// The purge deletes the overlay BEFORE the config: a failed overlay purge
// (permissions, containment) then leaves the config untouched, so the
// environment stays declared and a retry of `env rm --purge` still sees it. The
// reverse order stranded the user — config gone, overlay left behind, nothing
// declared to retry against. The intermediate state of this order (overlay
// removed, config still present) is a declared environment without an overlay,
// which is legal.
func removeEnvironment(
	cmd *cobra.Command,
	repoRoot, configRel, overlayRel string,
	purge bool,
) error {
	out := cmd.OutOrStdout()

	if purge {
		removed, err := environment.RemoveOverlay(repoRoot, overlayRel)
		if err != nil {
			return fmt.Errorf("removing environment overlay: %w", err)
		}

		if removed {
			notify.Activityf(out, "removed %s", overlayRel)
		} else {
			notify.Infof(out, "no overlay at %s: nothing to purge", overlayRel)
		}
	}

	err := environment.RemoveEnvironmentConfig(repoRoot, configRel)
	if err != nil {
		// Enrich with the declared environments so a mistyped name on the
		// default (no-config-load) path still reports what is available.
		return enrichSourceConfigError(
			cmd, repoRoot, fmt.Errorf("removing environment config: %w", err),
		)
	}

	notify.Activityf(out, "removed %s", configRel)

	if !purge && overlayRel != "" && overlayExists(repoRoot, overlayRel) {
		notify.Infof(out, "overlay %s retained (use --purge to also delete it)", overlayRel)
	}

	notify.Successf(out, "removed environment %q", environmentNameFromConfig(configRel))

	return nil
}

// overlayExists reports whether the environment's overlay directory is present,
// so the retained-overlay hint only prints when there is something to retain.
func overlayExists(repoRoot, overlayRel string) bool {
	info, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(overlayRel)))

	return err == nil && info.IsDir()
}

// environmentNameFromConfig recovers the environment name from its config file
// name (ksail.<name>.yaml), so the success line names the environment rather
// than the file.
func environmentNameFromConfig(configRel string) string {
	name := configRel
	name = name[len("ksail."):]
	name = name[:len(name)-len(".yaml")]

	return name
}
