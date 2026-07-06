package workload

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload/cipher"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload/gen"
	"github.com/spf13/cobra"
)

// permissionWrite is the annotations.AnnotationPermission value that marks a
// command as state-modifying (and therefore requiring user confirmation).
const permissionWrite = "write"

// Command group IDs used to organize `workload --help` into themed sections.
// Groups are help-rendering only and do not affect command names, flags, or the
// AI tool surface.
const (
	groupResources = "resources"
	groupImages    = "images"
	groupGitOps    = "gitops"
	groupDevLoop   = "devloop"
	groupSecrets   = "secrets"
)

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
			"reconcile, rollout, scale, watch\n" +
			"  cipher    - Manage SOPS-encrypted secret files (encrypt, decrypt, edit, import, rotate)\n\n" +
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

	cmd.AddGroup(
		&cobra.Group{ID: groupResources, Title: "Resources:"},
		&cobra.Group{ID: groupImages, Title: "Images:"},
		&cobra.Group{ID: groupGitOps, Title: "GitOps:"},
		&cobra.Group{ID: groupDevLoop, Title: "Dev loop:"},
		&cobra.Group{ID: groupSecrets, Title: "Secrets:"},
	)

	addWorkloadSubcommands(cmd)

	return cmd
}

// addWorkloadSubcommands registers every workload subcommand under its help
// group. Grouping is help-rendering only and does not affect the AI tool
// surface (which is driven by command Use/Short/flags, not GroupID).
func addWorkloadSubcommands(cmd *cobra.Command) {
	addGroupedCommand(cmd, NewGetCmd(), groupResources)
	addGroupedCommand(cmd, NewDescribeCmd(), groupResources)
	addGroupedCommand(cmd, NewExplainCmd(), groupResources)
	addGroupedCommand(cmd, NewApplyCmd(), groupResources)
	addGroupedCommand(cmd, NewCreateCmd(), groupResources)
	addGroupedCommand(cmd, NewDeleteCmd(), groupResources)
	addGroupedCommand(cmd, NewEditCmd(), groupResources)
	addGroupedCommand(cmd, NewExposeCmd(), groupResources)
	addGroupedCommand(cmd, NewScaleCmd(), groupResources)
	addGroupedCommand(cmd, NewWaitCmd(), groupResources)
	addGroupedCommand(cmd, gen.NewGenCmd(), groupResources)

	addGroupedCommand(cmd, NewImagesCmd(), groupImages)
	addGroupedCommand(cmd, NewExportCmd(), groupImages)
	addGroupedCommand(cmd, NewImportCmd(), groupImages)

	addGroupedCommand(cmd, NewReconcileCmd(), groupGitOps)
	addGroupedCommand(cmd, NewPushCmd(), groupGitOps)
	addGroupedCommand(cmd, NewInstallCmd(), groupGitOps)
	addGroupedCommand(cmd, NewValidateCmd(), groupGitOps)
	addGroupedCommand(cmd, NewScanCmd(), groupGitOps)

	addGroupedCommand(cmd, NewWatchCmd(), groupDevLoop)
	addGroupedCommand(cmd, NewDebugCmd(), groupDevLoop)
	addGroupedCommand(cmd, NewExecCmd(), groupDevLoop)
	addGroupedCommand(cmd, NewForwardCmd(), groupDevLoop)
	addGroupedCommand(cmd, NewLogsCmd(), groupDevLoop)
	addGroupedCommand(cmd, NewMirrorCmd(), groupDevLoop)
	addGroupedCommand(cmd, NewInterceptCmd(), groupDevLoop)
	addGroupedCommand(cmd, NewNetworkCmd(), groupDevLoop)
	addGroupedCommand(cmd, NewRolloutCmd(), groupDevLoop)

	addGroupedCommand(cmd, cipher.NewCipherCmd(), groupSecrets)
}

// addGroupedCommand assigns child to the given help group and adds it to parent.
func addGroupedCommand(parent, child *cobra.Command, groupID string) {
	child.GroupID = groupID

	parent.AddCommand(child)
}
