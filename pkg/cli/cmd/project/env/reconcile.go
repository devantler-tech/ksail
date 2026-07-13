package env

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	kustomizationgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/kustomization"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/spf13/cobra"
)

// reconcileLongDesc documents the reconcile semantics: read-side plan first
// (what is declared vs. what exists), then generation of the missing overlays
// only — never an overwrite, never a deletion.
const reconcileLongDesc = `Reconcile the declared cluster environments with their overlays.

Derives the workspace's reconcile plan — which declared environments
(ksail.<name>.yaml) have their clusters/<name>/ overlay, which are missing it,
and which overlay directories no environment declares (orphans) — prints it,
and scaffolds the missing overlays (the shared clusters/base plus a
clusters/<name> kustomization referencing it).

Generation never overwrites: existing files are preserved (a second run reports
the same paths while rewriting nothing) and orphan overlays are only surfaced,
never deleted — resolving an orphan is the operator's call ("ksail project env
add" to declare it, or "ksail project env rm --purge" semantics to remove it).

Examples:
  # Print the plan and scaffold the missing overlays
  ksail project env reconcile --experimental`

// NewReconcileCmd creates and returns the `project env reconcile` command.
func NewReconcileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "reconcile",
		Short:        "Reconcile declared cluster environments with their overlays",
		Long:         reconcileLongDesc,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return HandleReconcileRunE(cmd)
		},
	}

	// env reconcile is a net-new, state-modifying (file-writing) command, so it
	// ships behind the experimental gate per the repo's feature-flag-first
	// convention (the visible-command carve-out covers only low-risk read-only
	// additions like env list). Graduate by dropping this Guard call once the
	// command has settled through a release.
	return experimental.Guard(cmd)
}

// HandleReconcileRunE handles the `project env reconcile` command. It resolves
// the workspace root, derives the reconcile plan (loading configs the same
// silent, validation-skipping way the other env verbs do), prints it, and
// scaffolds the missing overlays with force hardwired off. Exported for
// testing.
func HandleReconcileRunE(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Canonicalise so the workspace root matches the symlink-resolved paths the
	// config loader derives, mirroring the other env verbs.
	repoRoot, err := fsutil.EvalCanonicalPath(workDir)
	if err != nil {
		return fmt.Errorf("failed to resolve current directory: %w", err)
	}

	// The plan spans the whole workspace, so the source directory comes from
	// the base ksail.yaml (the same root config the multi-cluster scaffold
	// derives the tree from), not from any single environment's config.
	baseCfg, err := loadEnvironmentConfig(cmd, "ksail.yaml")
	if err != nil {
		return fmt.Errorf("loading workspace base config: %w", err)
	}

	sourceDir, err := repoRelativeSourceDir(repoRoot, baseCfg.Spec.Workload.SourceDirectory)
	if err != nil {
		return err
	}

	loader := func(configFile string) (*v1alpha1.Cluster, error) {
		return loadEnvironmentConfig(cmd, configFile)
	}

	plan, err := environment.DerivePlan(repoRoot, sourceDir, loader)
	if err != nil {
		return fmt.Errorf("deriving reconcile plan: %w", err)
	}

	displayPlan(out, plan)

	return generateMissing(out, repoRoot, sourceDir, plan)
}

// displayPlan prints one line per declared environment with its overlay state,
// then the orphan overlays nothing declares.
func displayPlan(out io.Writer, plan environment.Plan) {
	if len(plan.Entries) == 0 {
		notify.Infof(
			out,
			"no environments declared; scaffold one with "+
				"`ksail project env add <name> --from <env>`",
		)
	} else {
		writer := tabwriter.NewWriter(out, 0, listEnvTabSize, listEnvTabPadding, ' ', 0)

		_, _ = fmt.Fprintln(writer, "ENVIRONMENT\tOVERLAY\tSTATE")

		for _, entry := range plan.Entries {
			_, _ = fmt.Fprintf(
				writer, "%s\t%s\t%s\n",
				entry.Environment.Name, entry.OverlayDir, entry.State,
			)
		}

		_ = writer.Flush()
	}

	for _, orphan := range plan.Orphans {
		notify.Warningf(
			out,
			"orphan overlay %s: no environment declares it (left untouched)",
			orphan,
		)
	}
}

// generateMissing scaffolds the plan's Missing overlays and reports each
// resolved path repo-relative. The writer is idempotent (force hardwired off),
// so an existing file is preserved and still reported.
func generateMissing(out io.Writer, repoRoot, sourceDir string, plan environment.Plan) error {
	missing := 0

	for _, entry := range plan.Entries {
		if entry.State == environment.OverlayMissing {
			missing++
		}
	}

	if missing == 0 {
		notify.Successf(out, "all declared environments have their overlay; nothing to generate")

		return nil
	}

	written, err := environment.GenerateMissingOverlays(
		kustomizationgenerator.NewGenerator(),
		filepath.Join(repoRoot, filepath.FromSlash(sourceDir)),
		plan,
	)
	if err != nil {
		return fmt.Errorf("generating missing overlays: %w", err)
	}

	for _, file := range written {
		rel, relErr := filepath.Rel(repoRoot, file)
		if relErr != nil {
			rel = file
		}

		notify.Generatef(out, "ensured %s", filepath.ToSlash(rel))
	}

	notify.Successf(
		out,
		"reconciled %d missing environment overlay(s)",
		missing,
	)

	return nil
}
