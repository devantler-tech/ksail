package workload

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload/gen"
	"github.com/spf13/cobra"
)

// permissionWrite is the annotations.AnnotationPermission value that marks a
// command as state-modifying (and therefore requiring user confirmation).
const permissionWrite = "write"

// NewWorkloadCmd creates and returns the workload command group namespace.
func NewWorkloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workload",
		Short: "Manage workload operations",
		Long: "Manage workload operations including resource inspection, " +
			"GitOps reconciliation, and lifecycle management.\n\n" +
			"Read operations:\n" +
			"  get       - List resources with optional -o json for structured output including status/conditions\n" +
			"  describe  - Show detailed resource info including events, conditions, and error details\n" +
			"  logs      - Print container logs (use --tail=N, --previous for crash diagnostics)\n" +
			"  explain   - Show API documentation for a resource kind\n" +
			"  forward   - Forward one or more local ports to a pod\n" +
			"  images    - List container images required by cluster components\n" +
			"  scan      - Run security scans on Kubernetes manifests using Kubescape\n" +
			"  wait      - Wait for a specific condition on resources\n\n" +
			"Write operations:\n" +
			"  apply, create, debug, delete, edit, exec, export, expose, import, install, push, " +
			"reconcile, rollout, scale, watch\n\n" +
			"GitOps diagnostics: Use 'get' with Flux resources (kustomization, helmrelease, " +
			"ocirepository -A -o json) or ArgoCD resources (application -A -o json) to check " +
			"reconciliation status, health, and errors in a single call.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
		Annotations: map[string]string{
			// Consolidate workload subcommands into tools split by permission:
			// workload_read and workload_write.
			// The "workload_command" parameter will select which command to execute.
			annotations.AnnotationConsolidate: "workload_command",
		},
	}

	cmd.AddCommand(NewReconcileCmd())
	cmd.AddCommand(NewPushCmd())
	cmd.AddCommand(NewApplyCmd())
	cmd.AddCommand(NewCreateCmd())
	cmd.AddCommand(NewDebugCmd())
	cmd.AddCommand(NewDeleteCmd())
	cmd.AddCommand(NewDescribeCmd())
	cmd.AddCommand(NewEditCmd())
	cmd.AddCommand(NewExecCmd())
	cmd.AddCommand(NewExplainCmd())
	cmd.AddCommand(NewExportCmd())
	cmd.AddCommand(NewExposeCmd())
	cmd.AddCommand(NewForwardCmd())
	cmd.AddCommand(NewGetCmd())
	cmd.AddCommand(gen.NewGenCmd())
	cmd.AddCommand(NewImagesCmd())
	cmd.AddCommand(NewImportCmd())
	cmd.AddCommand(NewInstallCmd())
	cmd.AddCommand(NewLogsCmd())
	cmd.AddCommand(NewRolloutCmd())
	cmd.AddCommand(NewScaleCmd())
	cmd.AddCommand(NewScanCmd())
	cmd.AddCommand(NewValidateCmd())
	cmd.AddCommand(NewWaitCmd())
	cmd.AddCommand(NewWatchCmd())

	return cmd
}
