package workload

import (
	"github.com/devantler-tech/ksail/cmd/workload/gen"
	runtime "github.com/devantler-tech/ksail/pkg/di"
	"github.com/spf13/cobra"
)

// NewWorkloadCmd creates and returns the workload command group namespace.
func NewWorkloadCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workload",
		Short: "Manage workload operations",
		Long: "Group workload commands under a single namespace to reconcile, apply, create, delete, describe, edit, exec, " +
			"explain, expose, get, gen, install, logs, rollout, scale, validate, or wait for workloads.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
	}

	cmd.AddCommand(NewReconcileCmd(runtimeContainer))
	cmd.AddCommand(NewApplyCmd())
	cmd.AddCommand(NewCreateCmd(runtimeContainer))
	cmd.AddCommand(NewDeleteCmd())
	cmd.AddCommand(NewDescribeCmd())
	cmd.AddCommand(NewEditCmd())
	cmd.AddCommand(NewExecCmd())
	cmd.AddCommand(NewExplainCmd())
	cmd.AddCommand(NewExposeCmd())
	cmd.AddCommand(NewGetCmd())
	cmd.AddCommand(gen.NewGenCmd(runtimeContainer))
	cmd.AddCommand(NewInstallCmd(runtimeContainer))
	cmd.AddCommand(NewLogsCmd())
	cmd.AddCommand(NewRolloutCmd())
	cmd.AddCommand(NewScaleCmd())
	cmd.AddCommand(NewValidateCmd())
	cmd.AddCommand(NewWaitCmd())

	return cmd
}
