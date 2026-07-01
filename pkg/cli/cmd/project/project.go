package project

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/spf13/cobra"
)

// NewProjectCmd creates the parent project command and wires subcommands beneath
// it. The project group hosts commands that operate solely on the GitOps project
// files (scaffolding, environment cloning) with no live cluster involved. Its
// first subcommand is add-environment, which moved here from `cluster` (see issue
// #5626); the remaining file-operating commands (init) follow in later increments.
//
// Now that the group carries a subcommand it joins the generated MCP/chat tool
// surface via the toolgen consolidate annotation (mirroring the cluster group),
// so `project add-environment` is exposed as a tool.
func NewProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage GitOps project files",
		Long: `Manage the GitOps project files (the on-disk project structure) without ` +
			`contacting a live cluster. These commands scaffold and transform the project ` +
			`— e.g. initializing a new project and cloning environments — as opposed to ` +
			`the cluster commands that operate on a running cluster.`,
		Args:         cobra.NoArgs,
		RunE:         handleProjectRunE,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationConsolidate: "command",
		},
	}

	cmd.AddCommand(NewAddEnvironmentCmd())

	return cmd
}

//nolint:gochecknoglobals // Injected for testability to simulate help failures.
var helpRunner = func(cmd *cobra.Command) error {
	return cmd.Help()
}

func handleProjectRunE(cmd *cobra.Command, _ []string) error {
	err := helpRunner(cmd)
	if err != nil {
		return fmt.Errorf("displaying project command help: %w", err)
	}

	return nil
}
