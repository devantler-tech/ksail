package chat

import (
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
)

// GetKSailToolMetadata returns both the Copilot tools and their metadata.
// This allows callers to access RequiresPermission and other metadata.
// The sessionLog parameter enables SDK-native session logging from tool handlers (can be nil).
func GetKSailToolMetadata(
	rootCmd *cobra.Command,
	outputChan chan<- toolgen.OutputChunk,
	sessionLog *toolgen.SessionLogRef,
) (
	[]copilot.Tool, map[string]toolgen.ToolDefinition,
) {
	// Generate tools from the Cobra command tree
	opts := toolgen.DefaultOptions()
	opts.OutputChan = outputChan
	opts.SessionLog = sessionLog
	// Run tools via the running binary instead of a PATH lookup of "ksail",
	// so chat tool calls work regardless of how ksail was launched.
	opts.ExecutablePath = toolgen.DefaultExecutablePath()

	// Get SDK-agnostic tool definitions
	toolDefs := toolgen.GenerateTools(rootCmd, opts)

	// Build metadata map
	metadata := make(map[string]toolgen.ToolDefinition, len(toolDefs))
	for _, def := range toolDefs {
		metadata[def.Name] = def
	}

	// Convert SDK-agnostic definitions to Copilot SDK format
	tools := toolgen.ToCopilotTools(toolDefs, opts)

	return tools, metadata
}
