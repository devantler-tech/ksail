package cluster

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewClusterCmd creates the parent cluster command and wires lifecycle subcommands beneath it.
func NewClusterCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage cluster lifecycle",
		Long: `Manage lifecycle operations for local Kubernetes clusters, including ` +
			`provisioning, teardown, and status.`,
		Args:         cobra.NoArgs,
		RunE:         handleClusterRunE,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationConsolidate: "command",
		},
	}

	cmd.AddCommand(NewInitCmd(runtimeContainer))
	cmd.AddCommand(NewCreateCmd(runtimeContainer))
	cmd.AddCommand(NewUpdateCmd(runtimeContainer))
	cmd.AddCommand(NewDeleteCmd(runtimeContainer))
	cmd.AddCommand(NewStartCmd(runtimeContainer))
	cmd.AddCommand(NewStopCmd(runtimeContainer))
	cmd.AddCommand(NewListCmd(runtimeContainer))
	cmd.AddCommand(NewInfoCmd(runtimeContainer))
	cmd.AddCommand(NewConnectCmd(runtimeContainer))
	cmd.AddCommand(NewBackupCmd(runtimeContainer))
	cmd.AddCommand(NewRestoreCmd(runtimeContainer))

	return cmd
}

//nolint:gochecknoglobals // Injected for testability to simulate help failures.
var helpRunner = func(cmd *cobra.Command) error {
	return cmd.Help()
}

func handleClusterRunE(cmd *cobra.Command, _ []string) error {
	// Cobra Help() can return an error (e.g., output stream or template issues); wrap it for clarity.
	err := helpRunner(cmd)
	if err != nil {
		return fmt.Errorf("displaying cluster command help: %w", err)
	}

	return nil
}
