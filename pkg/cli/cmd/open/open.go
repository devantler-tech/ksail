package open

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/open/chat"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/open/mcp"
	"github.com/spf13/cobra"
)

const openLongDesc = `Open an interface to KSail for provisioning and managing clusters on your machine.

KSail surfaces the same functionality in several ways: a web UI in your browser, a native
desktop app, an interactive AI chat assistant, and an MCP server for external AI tools. Pick
whichever fits how you work — run a subcommand below to get started.`

// NewOpenCmd creates the parent 'open' command group and wires its subcommands beneath it.
func NewOpenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open a KSail interface (web UI, desktop app, AI chat, or MCP server)",
		Long:  openLongDesc,
		// Exclude from AI tool generation: every subcommand is a long-running, blocking
		// server / interactive session, not a discrete tool. Exclusion propagates to children.
		Annotations: map[string]string{
			annotations.AnnotationExclude: annotations.AnnotationValueTrue,
		},
	}

	cmd.AddCommand(NewWebCmd())
	cmd.AddCommand(NewDesktopCmd())
	cmd.AddCommand(chat.NewChatCmd())
	cmd.AddCommand(mcp.NewMCPCmd())

	return cmd
}
