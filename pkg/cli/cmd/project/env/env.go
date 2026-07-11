// Package env provides the `ksail project env` subcommand group: managing the
// cluster environments declared in a workspace (ksail.<name>.yaml root configs
// plus their clusters/<name>/ overlays) without contacting a live cluster. The
// group hosts the short verbs `add`, `list` and `rm`; the former flat
// `project add-environment` / `project list-environments` names remain as
// hidden, deprecated delegates in the parent package (issue #6057).
package env

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewEnvCmd creates the `project env` group command and wires the environment
// verbs beneath it. It mirrors the `workload gen` sub-package precedent: the
// group itself only prints help, and the parent project group's toolgen
// consolidate annotation folds the leaves into the project_read/project_write
// tools.
func NewEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage the cluster environments declared in the workspace",
		Long: `Manage the cluster environments declared in the workspace (the ` +
			`ksail.<name>.yaml root configs and their clusters/<name>/ overlays) ` +
			`without contacting a live cluster.`,
		Args:         cobra.NoArgs,
		RunE:         handleEnvRunE,
		SilenceUsage: true,
	}

	cmd.AddCommand(NewAddCmd())
	cmd.AddCommand(NewListCmd())
	cmd.AddCommand(NewRmCmd())

	return cmd
}

//nolint:gochecknoglobals // Injected for testability to simulate help failures.
var helpRunner = func(cmd *cobra.Command) error {
	return cmd.Help()
}

func handleEnvRunE(cmd *cobra.Command, _ []string) error {
	err := helpRunner(cmd)
	if err != nil {
		return fmt.Errorf("displaying env command help: %w", err)
	}

	return nil
}
