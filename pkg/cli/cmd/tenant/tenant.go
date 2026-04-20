package tenant

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/spf13/cobra"
)

// NewTenantCmd creates the parent tenant command and wires subcommands beneath it.
func NewTenantCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Manage tenant lifecycle",
		Long: `Manage multi-tenancy onboarding for Kubernetes clusters, including ` +
			`RBAC isolation, GitOps sync resources, and tenant repository scaffolding.`,
		Args:         cobra.NoArgs,
		RunE:         handleTenantRunE,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationConsolidate: "tenant_command",
		},
	}

	cmd.AddCommand(NewCreateCmd(runtimeContainer))
	cmd.AddCommand(NewDeleteCmd(runtimeContainer))

	return cmd
}

//nolint:gochecknoglobals // Injected for testability to simulate help failures.
var helpRunner = func(cmd *cobra.Command) error {
	return cmd.Help()
}

func handleTenantRunE(cmd *cobra.Command, _ []string) error {
	err := helpRunner(cmd)
	if err != nil {
		return fmt.Errorf("displaying tenant command help: %w", err)
	}

	return nil
}
