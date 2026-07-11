// Package env provides the `ksail project env` subcommand group: managing the
// cluster environments declared in a workspace (ksail.<name>.yaml root configs
// plus their clusters/<name>/ overlays) without contacting a live cluster. The
// group hosts the short verbs `add`, `list` and `rm`; the former flat
// `project add-environment` / `project list-environments` names remain as
// hidden, deprecated delegates in the parent package (issue #6057).
package env

import (
	"errors"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/spf13/cobra"
)

// ErrEmptyEnvironmentName is returned when an environment verb receives an
// empty name. [v1alpha1.ValidateClusterName] accepts "" (an empty cluster
// name means "use the default"), but for environments an empty name would
// silently target the malformed ksail..yaml — for `rm`, a destructive
// deletion of a file that was never a declared environment — so the env verbs
// reject it explicitly before any path is constructed.
var ErrEmptyEnvironmentName = errors.New("environment name must not be empty")

// validateEnvironmentName rejects empty names, then applies the cluster-name
// rules. Every env verb that turns a name into ksail.<name>.yaml or
// clusters/<name>/ paths validates through this shared gate.
func validateEnvironmentName(name string) error {
	if name == "" {
		return ErrEmptyEnvironmentName
	}

	err := v1alpha1.ValidateClusterName(name)
	if err != nil {
		//nolint:wrapcheck // The callers wrap with their own role context (source/destination).
		return err
	}

	return nil
}

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

// NewDeprecatedAddEnvironmentDelegate returns the former flat `add-environment`
// name as a hidden, deprecated delegate of `project env add` (issues #5626 and
// #6057). Both the cluster and project groups mount it, so the rebadging lives
// here once instead of byte-identical constructors in each group. Hidden keeps
// it out of help and the MCP/chat tool surface (toolgen skips hidden commands);
// the docs generator still emits its page — with the deprecation notice —
// matching the repo's other hidden commands.
func NewDeprecatedAddEnvironmentDelegate() *cobra.Command {
	cmd := NewAddCmd()
	cmd.Use = "add-environment <name>"
	cmd.Hidden = true
	cmd.Deprecated = `use "ksail project env add" instead`

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
