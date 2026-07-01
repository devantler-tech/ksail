package project

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/spf13/cobra"
)

// NewProjectCmd creates the parent project command and wires subcommands beneath
// it. The project group hosts commands that operate solely on the GitOps project
// files (scaffolding, environment cloning) with no live cluster involved. It is
// currently a group shell; the file-operating commands (init, add-environment)
// move under it in follow-up increments (see issue #5626).
//
// While the group carries no subcommands it is excluded from the generated
// MCP/chat tool surface (there is nothing to invoke yet); the toolgen
// consolidate annotation is added together with the first subcommand.
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
			annotations.AnnotationExclude: annotations.AnnotationValueTrue,
		},
	}

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
