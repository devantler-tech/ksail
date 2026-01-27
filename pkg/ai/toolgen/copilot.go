package toolgen

import (
	"context"
	"fmt"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// ToCopilotTools converts tool definitions to Copilot SDK tools.
func ToCopilotTools(tools []ToolDefinition, opts ToolOptions) []copilot.Tool {
	copilotTools := make([]copilot.Tool, 0, len(tools))

	for _, tool := range tools {
		copilotTools = append(copilotTools, toCopilotTool(tool, opts))
	}

	return copilotTools
}

// toCopilotTool converts a single tool definition to a Copilot SDK tool.
func toCopilotTool(tool ToolDefinition, opts ToolOptions) copilot.Tool {
	return copilot.Tool{
		Name:        tool.Name,
		Description: tool.Description,
		Parameters:  tool.Parameters,
		Handler:     buildCopilotHandler(tool, opts),
	}
}

// buildCopilotHandler creates a Copilot SDK handler from a tool definition.
func buildCopilotHandler(tool ToolDefinition, opts ToolOptions) copilot.ToolHandler {
	return func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
		// Extract parameters
		params, ok := invocation.Arguments.(map[string]any)
		if !ok {
			params = make(map[string]any)
		}

		// Build the full command string for reporting
		fullCmd := tool.CommandPath

		if len(params) > 0 {
			argStrs := formatParametersForDisplay(params)
			if len(argStrs) > 0 {
				fullCmd += " " + strings.Join(argStrs, " ")
			}
		}

		// Execute the command
		// Note: We create our own context since Copilot SDK ToolHandler
		// doesn't provide a context parameter
		ctx := context.Background()
		err := ExecuteTool(ctx, tool, params, opts)

		// If we have an output channel, we've already streamed the output
		// Return a simple result
		if opts.OutputChan != nil {
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf(
						"Command: %s\nStatus: FAILED\nError: %v",
						fullCmd,
						err,
					),
					ResultType:    "failure",
					SessionLog:    fmt.Sprintf("[FAILED] %s: %v", fullCmd, err),
					ToolTelemetry: map[string]any{},
				}, nil
			}

			return copilot.ToolResult{
				TextResultForLLM: fmt.Sprintf(
					"Command: %s\nStatus: SUCCESS",
					fullCmd,
				),
				ResultType:    "success",
				SessionLog:    "[SUCCESS] " + fullCmd,
				ToolTelemetry: map[string]any{},
			}, nil
		}

		// For non-streaming, err will contain the output in the error message
		if err != nil {
			return copilot.ToolResult{
				TextResultForLLM: fmt.Sprintf(
					"Command: %s\nStatus: FAILED\nError: %v",
					fullCmd,
					err,
				),
				ResultType:    "failure",
				SessionLog:    fmt.Sprintf("[FAILED] %s: %v", fullCmd, err),
				ToolTelemetry: map[string]any{},
			}, nil
		}

		return copilot.ToolResult{
			TextResultForLLM: fmt.Sprintf(
				"Command: %s\nStatus: SUCCESS",
				fullCmd,
			),
			ResultType:    "success",
			SessionLog:    "[SUCCESS] " + fullCmd,
			ToolTelemetry: map[string]any{},
		}, nil
	}
}

// formatParametersForDisplay converts parameters map to readable strings for logging.
func formatParametersForDisplay(params map[string]any) []string {
	argStrs := make([]string, 0, len(params))

	for name, value := range params {
		if name == "args" {
			// Positional arguments
			if args, ok := value.([]any); ok {
				for _, arg := range args {
					argStrs = append(argStrs, fmt.Sprintf("%v", arg))
				}
			}
		} else {
			// Flag arguments
			switch v := value.(type) {
			case bool:
				if v {
					argStrs = append(argStrs, "--"+name)
				}
			case []any:
				for _, item := range v {
					argStrs = append(argStrs, fmt.Sprintf("--%s=%v", name, item))
				}
			case nil:
				// Skip nil
			default:
				argStrs = append(argStrs, fmt.Sprintf("--%s=%v", name, value))
			}
		}
	}

	return argStrs
}
