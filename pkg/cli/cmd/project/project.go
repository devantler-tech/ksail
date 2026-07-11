package project

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
	"github.com/spf13/cobra"
)

// permissionWrite is the annotations.AnnotationPermission value that marks a
// command as state-modifying (and therefore requiring user confirmation).
const permissionWrite = "write"

// NewProjectCmd creates the parent project command and wires subcommands beneath
// it. The project group hosts commands that operate solely on the GitOps project
// files (scaffolding, environment management) with no live cluster involved:
// init scaffolds a new project and the `env` group manages the declared cluster
// environments (issue #6057; init and environment cloning originally moved here
// from `cluster`, see issue #5626).
//
// The group carries subcommands so it joins the generated MCP/chat tool surface
// via the toolgen consolidate annotation (mirroring the cluster group), so
// `project init` and the `project env` verbs are exposed as tools.
func NewProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage GitOps project files",
		Long: `Manage the GitOps project files (the on-disk project structure) without ` +
			`contacting a live cluster. These commands scaffold and transform the project ` +
			`— e.g. initializing a new project and managing its cluster environments — as ` +
			`opposed to the cluster commands that operate on a running cluster.`,
		Args:         cobra.NoArgs,
		RunE:         handleProjectRunE,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationConsolidate: "command",
		},
	}

	cmd.AddCommand(NewInitCmd())
	cmd.AddCommand(env.NewEnvCmd())
	cmd.AddCommand(env.NewDeprecatedAddEnvironmentDelegate())
	cmd.AddCommand(newDeprecatedListEnvironmentsCmd())

	return cmd
}

// newDeprecatedListEnvironmentsCmd returns the former flat `project
// list-environments` name as a hidden, deprecated delegate of `project env
// list` (issue #6057). Hidden keeps it out of help and the MCP/chat tool
// surface (toolgen skips hidden commands); the docs generator still emits its
// page with the deprecation notice, matching the repo's other hidden commands.
// The `ls` alias is stripped so the short form belongs to
// the canonical `project env ls` only.
func newDeprecatedListEnvironmentsCmd() *cobra.Command {
	cmd := env.NewListCmd()
	cmd.Use = "list-environments"
	cmd.Aliases = nil
	cmd.Hidden = true
	cmd.Deprecated = `use "ksail project env list" instead`

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
