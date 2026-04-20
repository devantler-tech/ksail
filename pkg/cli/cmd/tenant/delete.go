package tenant

import (
	"fmt"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates the tenant delete subcommand.
func NewDeleteCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <tenant-name>",
		Short: "Delete a tenant",
		Long: `Remove tenant manifests, unregister from ` +
			`kustomization.yaml, and optionally delete ` +
			`the tenant Git repository.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.Flags().BoolP("force", "f", false, "Skip all confirmation prompts")
	cmd.Flags().Bool("unregister", true, "Remove tenant from kustomization.yaml")
	cmd.Flags().String("kustomization-path", "", "Path to kustomization.yaml")
	cmd.Flags().Bool("delete-repo", false, "Also delete the tenant Git repository")
	cmd.Flags().String("git-provider", "", "Git provider (required with --delete-repo)")
	cmd.Flags().
		String("tenant-repo", "", "Tenant repo as owner/repo-name (required with --delete-repo)")
	cmd.Flags().String("git-token", "", "Git provider API token")
	cmd.Flags().StringP("output", "o", ".", "Directory containing tenant manifests")

	cmd.RunE = handleDeleteRunE

	return cmd
}

func handleDeleteRunE(cmd *cobra.Command, args []string) error {
	opts := tenant.DeleteOptions{
		Name: args[0],
	}

	force, _ := cmd.Flags().GetBool("force")
	opts.Unregister, _ = cmd.Flags().GetBool("unregister")
	opts.KustomizationPath, _ = cmd.Flags().GetString("kustomization-path")
	opts.DeleteRepo, _ = cmd.Flags().GetBool("delete-repo")
	opts.GitProvider, _ = cmd.Flags().GetString("git-provider")
	opts.TenantRepo, _ = cmd.Flags().GetString("tenant-repo")
	opts.GitToken, _ = cmd.Flags().GetString("git-token")

	outputStr, _ := cmd.Flags().GetString("output")

	outputDir, err := fsutil.EvalCanonicalPath(outputStr)
	if err != nil {
		return fmt.Errorf("resolving output path: %w", err)
	}

	opts.OutputDir = outputDir

	if !confirm.ShouldSkipPrompt(force) {
		if opts.DeleteRepo && opts.TenantRepo != "" {
			notify.Warningf(cmd.OutOrStdout(),
				"This will delete tenant %q, its manifest directory, "+
					"and the remote Git repository %q",
				opts.Name, opts.TenantRepo)
		} else {
			notify.Warningf(cmd.OutOrStdout(),
				"This will delete tenant %q and its manifest directory",
				opts.Name)
		}

		_, _ = fmt.Fprint(cmd.OutOrStdout(),
			`Type "yes" to confirm deletion: `)

		if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
			return confirm.ErrDeletionCancelled
		}
	}

	err = tenant.Delete(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("deleting tenant: %w", err)
	}

	// Best-effort: remove tenant's ArgoCD RBAC policy from argocd-rbac-cm.
	if opts.Unregister {
		removeArgoCDRBACPolicy(cmd, opts)
	}

	notify.Successf(cmd.OutOrStdout(), "Tenant %q deleted successfully", opts.Name)

	return nil
}

// removeArgoCDRBACPolicy is a best-effort cleanup of ArgoCD RBAC policies.
// Path resolution and discovery failures are silently skipped (expected when
// no kustomization.yaml or argocd-rbac-cm file exists). Write/parse failures
// emit a warning but do not block tenant deletion.
func removeArgoCDRBACPolicy(cmd *cobra.Command, opts tenant.DeleteOptions) {
	kPath, err := tenant.ResolveKustomizationPath(opts.OutputDir, opts.KustomizationPath)
	if err != nil {
		return
	}

	kDir := filepath.Dir(kPath)

	rbacCMPath, err := tenant.FindArgoCDRBACCM(kDir)
	if err != nil || rbacCMPath == "" {
		return
	}

	err = tenant.RemoveArgoCDRBACPolicyFile(rbacCMPath, opts.Name)
	if err != nil {
		notify.Warningf(cmd.OutOrStdout(),
			"failed to remove ArgoCD RBAC policy for tenant %q: %v", opts.Name, err)
	}
}
