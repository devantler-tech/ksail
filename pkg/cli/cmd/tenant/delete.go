package tenant

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates the tenant delete subcommand.
func NewDeleteCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "delete <tenant-name>",
		Short:        "Delete a tenant",
		Long:         `Remove tenant manifests, unregister from kustomization.yaml, and optionally delete the tenant Git repository.`,
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
	cmd.Flags().String("git-repo", "", "Tenant repo as owner/repo-name (required with --delete-repo)")
	cmd.Flags().String("git-token", "", "Git provider API token")
	cmd.Flags().StringP("output", "o", ".", "Directory containing tenant manifests")

	cmd.RunE = handleDeleteRunE

	return cmd
}

func handleDeleteRunE(cmd *cobra.Command, args []string) error {
	opts := tenant.DeleteOptions{
		Name: args[0],
	}

	opts.Force, _ = cmd.Flags().GetBool("force")
	opts.Unregister, _ = cmd.Flags().GetBool("unregister")
	opts.KustomizationPath, _ = cmd.Flags().GetString("kustomization-path")
	opts.DeleteRepo, _ = cmd.Flags().GetBool("delete-repo")
	opts.GitProvider, _ = cmd.Flags().GetString("git-provider")
	opts.GitRepo, _ = cmd.Flags().GetString("git-repo")
	opts.GitToken, _ = cmd.Flags().GetString("git-token")

	outputStr, _ := cmd.Flags().GetString("output")

	outputDir, err := fsutil.EvalCanonicalPath(outputStr)
	if err != nil {
		return fmt.Errorf("resolving output path: %w", err)
	}

	opts.OutputDir = outputDir

	if err := tenant.Delete(opts); err != nil {
		return fmt.Errorf("deleting tenant: %w", err)
	}

	notify.Successf(cmd.OutOrStdout(), "Tenant %q deleted successfully", opts.Name)

	return nil
}
