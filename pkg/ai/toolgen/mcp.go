package toolgen

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp" //nolint:depguard // MCP SDK is required for MCP integration
)

// ToMCPTools converts tool definitions to MCP server tools.
// Each tool definition is registered with the MCP server.
func ToMCPTools(server *mcp.Server, tools []ToolDefinition, opts ToolOptions) {
	for _, tool := range tools {
		addMCPTool(server, tool, opts)
	}
}

// addMCPTool adds a single tool definition to an MCP server.
func addMCPTool(server *mcp.Server, tool ToolDefinition, opts ToolOptions) {
	// Create MCP tool definition
	mcpTool := &mcp.Tool{
		Name:        tool.Name,
		Description: tool.Description,
		// Expose the existing JSON schema to MCP clients for validation and UI generation.
		InputSchema: tool.Parameters,
	}

	// Create handler
	handler := func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input map[string]any,
	) (*mcp.CallToolResult, map[string]any, error) {
		// Execute the tool
		err := ExecuteTool(ctx, tool, input, opts)
		if err != nil {
			// MCP returns errors via IsError flag and error messages in content
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Command failed: %v", err),
					},
				},
			}, nil, nil
		}

		// Success - return empty content (output was streamed if OutputChan was set)
		return &mcp.CallToolResult{
			IsError: false,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Command '%s' completed successfully", tool.CommandPath),
				},
			},
		}, nil, nil
	}

	// Add tool to server
	mcp.AddTool(server, mcpTool, handler)
}
