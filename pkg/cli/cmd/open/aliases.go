package open

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/open/chat"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/open/mcp"
	"github.com/spf13/cobra"
)

// NewDeprecatedAliases returns the legacy top-level commands (`ui`, `desktop`, `chat`, `mcp`) as
// hidden, deprecated aliases of their `ksail open <name>` replacements. They are wired at the root
// for backward compatibility: each still runs the same logic but prints a deprecation notice that
// points at the new command. Being hidden, they are omitted from help, generated docs, and the AI
// tool surface. Remove them in a future major release.
func NewDeprecatedAliases() []*cobra.Command {
	aliases := []struct {
		use         string
		replacement string
		cmd         *cobra.Command
	}{
		{use: "ui", replacement: "ksail open web", cmd: NewWebCmd()},
		{use: "desktop", replacement: "ksail open desktop", cmd: NewDesktopCmd()},
		{use: "chat", replacement: "ksail open chat", cmd: chat.NewChatCmd()},
		{use: "mcp", replacement: "ksail open mcp", cmd: mcp.NewMCPCmd()},
	}

	cmds := make([]*cobra.Command, 0, len(aliases))

	for _, alias := range aliases {
		alias.cmd.Use = alias.use
		alias.cmd.Hidden = true
		alias.cmd.Deprecated = fmt.Sprintf("use %q instead", alias.replacement)
		cmds = append(cmds, alias.cmd)
	}

	return cmds
}
