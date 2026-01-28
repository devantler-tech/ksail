package chat

import (
	"github.com/devantler-tech/ksail/v5/pkg/ai/toolgen"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
)

// GetKSailTools returns the tools available to the chat assistant.
// It auto-generates tools from Cobra commands using the toolgen package.
// The rootCmd parameter should be the root Cobra command for the CLI.
// The outputChan parameter enables real-time output streaming (can be nil).
func GetKSailTools(rootCmd *cobra.Command, outputChan chan<- toolgen.OutputChunk) []copilot.Tool {
	// Generate tools from the Cobra command tree
	opts := toolgen.DefaultOptions()
	opts.OutputChan = outputChan

	// Get SDK-agnostic tool definitions from Cobra command tree
	toolDefs := toolgen.GenerateTools(rootCmd, opts)

	// Convert SDK-agnostic definitions to Copilot SDK format
	return toolgen.ToCopilotTools(toolDefs, opts)
}

// GetKSailToolMetadata returns both the Copilot tools and their metadata.
// This allows callers to access RequiresPermission and other metadata.
func GetKSailToolMetadata(rootCmd *cobra.Command, outputChan chan<- toolgen.OutputChunk) (
	[]copilot.Tool, map[string]toolgen.ToolDefinition,
) {
	// Generate tools from the Cobra command tree
	opts := toolgen.DefaultOptions()
	opts.OutputChan = outputChan

	// Get SDK-agnostic tool definitions
	toolDefs := toolgen.GenerateTools(rootCmd, opts)

	// Build metadata map
	metadata := make(map[string]toolgen.ToolDefinition, len(toolDefs))
	for _, def := range toolDefs {
		metadata[def.Name] = def
	}

	// Convert to Copilot SDK tools
	tools := toolgen.ToCopilotTools(toolDefs, opts)

	return tools, metadata
}
