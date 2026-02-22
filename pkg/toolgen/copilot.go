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
		fullCmd := buildFullCommand(tool.CommandPath, params)

		// Execute the command
		// Note: The Copilot SDK ToolHandler doesn't provide a context parameter,
		// so we create our own. This means parent context cancellation won't
		// stop running commands. Use CommandTimeout in ToolOptions for timeouts.
		ctx := context.Background()
		output, err := executeTool(ctx, tool, params, opts)

		return buildCopilotResult(fullCmd, output, err), nil
	}
}

// buildFullCommand constructs the full command string for display.
func buildFullCommand(commandPath string, params map[string]any) string {
	fullCmd := commandPath

	if len(params) > 0 {
		argStrs := formatParametersForDisplay(params)
		if len(argStrs) > 0 {
			fullCmd += " " + strings.Join(argStrs, " ")
		}
	}

	return fullCmd
}

// buildCopilotResult creates a Copilot ToolResult based on execution outcome.
func buildCopilotResult(fullCmd string, output string, err error) copilot.ToolResult {
	if err != nil {
		var errText strings.Builder
		errText.Grow(len("Command: \nStatus: FAILED\nError: ") + len(fullCmd) + len(err.Error()))
		errText.WriteString("Command: ")
		errText.WriteString(fullCmd)
		errText.WriteString("\nStatus: FAILED\nError: ")
		errText.WriteString(err.Error())

		return copilot.ToolResult{
			TextResultForLLM: errText.String(),
			ResultType:       "failure",
			SessionLog:       "[FAILED] " + fullCmd + ": " + err.Error(),
			ToolTelemetry:    map[string]any{},
		}
	}

	var result strings.Builder
	result.Grow(len("Command: \nStatus: SUCCESS\nOutput:\n") + len(fullCmd) + len(output))

	result.WriteString("Command: ")
	result.WriteString(fullCmd)
	result.WriteString("\nStatus: SUCCESS")

	if output != "" {
		result.WriteString("\nOutput:\n")
		result.WriteString(output)
	}

	return copilot.ToolResult{
		TextResultForLLM: result.String(),
		ResultType:       "success",
		SessionLog:       "[SUCCESS] " + fullCmd,
		ToolTelemetry:    map[string]any{},
	}
}

// formatParametersForDisplay converts parameters map to readable strings for logging.
func formatParametersForDisplay(params map[string]any) []string {
	argStrs := make([]string, 0, len(params))

	for name, value := range params {
		if name == "args" {
			argStrs = append(argStrs, formatPositionalArgs(value)...)
		} else {
			argStrs = append(argStrs, formatFlagArg(name, value)...)
		}
	}

	return argStrs
}

// formatPositionalArgs converts positional arguments to strings.
func formatPositionalArgs(value any) []string {
	args, ok := value.([]any)
	if !ok {
		return nil
	}

	result := make([]string, 0, len(args))
	for _, arg := range args {
		result = append(result, fmt.Sprintf("%v", arg))
	}

	return result
}
