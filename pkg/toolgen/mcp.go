package toolgen

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToMCPTools converts tool definitions to MCP server tools.
// Each tool definition is registered with the MCP server.
// Note: mcp.AddTool does not return errors; registration failures will panic.
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
		output, err := executeTool(ctx, tool, input, opts)
		if err != nil {
			// MCP returns errors via IsError flag and error messages in content
			// Include both the captured output (which contains the actual error details)
			// and the error message (which contains the exit code)
			var b strings.Builder
			b.Grow(len("Command '' failed") + len(tool.CommandPath) + len(output) + 50)
			b.WriteString("Command '")
			b.WriteString(tool.CommandPath)
			b.WriteString("' failed")
			if output != "" {
				b.WriteString("\nOutput:\n")
				b.WriteString(output)
			}
			b.WriteString("\nError: ")
			b.WriteString(err.Error())

			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: b.String(),
					},
				},
			}, nil, nil
		}

		// Success - include command output in response
		var b strings.Builder
		b.Grow(len("Command '' completed successfully") + len(tool.CommandPath) + len(output) + 10)
		b.WriteString("Command '")
		b.WriteString(tool.CommandPath)
		b.WriteString("' completed successfully")
		if output != "" {
			b.WriteString("\nOutput:\n")
			b.WriteString(output)
		}

		return &mcp.CallToolResult{
			IsError: false,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: b.String(),
				},
			},
		}, nil, nil
	}

	// Add tool to server.
	// Note: mcp.AddTool may panic on registration failures such as:
	//   - Duplicate tool names (tool already registered)
	//   - Invalid tool definitions (nil server, malformed schema)
	//   - Internal MCP server errors
	// This is acceptable for server initialization where failures should be fatal.
	// The panic will propagate up and terminate the MCP server startup process.
	mcp.AddTool(server, mcpTool, handler)
}
