package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewWorkloadCmd creates and returns the workload command group namespace.
func NewWorkloadCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workload",
		Short: "Manage workload operations",
		Long: "Group workload commands under a single namespace to reconcile, apply, create, delete, describe, edit, exec, " +
			"explain, export, expose, gen, get, import, install, logs, push, rollout, scale, validate, or wait for workloads.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
	}

	cmd.AddCommand(NewReconcileCmd(runtimeContainer))
	cmd.AddCommand(NewPushCmd(runtimeContainer))
	cmd.AddCommand(NewApplyCmd())
	cmd.AddCommand(NewCreateCmd(runtimeContainer))
	cmd.AddCommand(NewDeleteCmd())
	cmd.AddCommand(NewDescribeCmd())
	cmd.AddCommand(NewEditCmd())
	cmd.AddCommand(NewExecCmd())
	cmd.AddCommand(NewExplainCmd())
	cmd.AddCommand(NewExportCmd(runtimeContainer))
	cmd.AddCommand(NewExposeCmd())
	cmd.AddCommand(NewGetCmd())
	cmd.AddCommand(gen.NewGenCmd(runtimeContainer))
	cmd.AddCommand(NewImportCmd(runtimeContainer))
	cmd.AddCommand(NewInstallCmd(runtimeContainer))
	cmd.AddCommand(NewLogsCmd())
	cmd.AddCommand(NewRolloutCmd())
	cmd.AddCommand(NewScaleCmd())
	cmd.AddCommand(NewValidateCmd())
	cmd.AddCommand(NewWaitCmd())

	return cmd
}
