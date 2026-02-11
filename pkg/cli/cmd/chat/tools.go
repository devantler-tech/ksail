package chat

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	"github.com/devantler-tech/ksail/v5/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
)

// getToolName extracts the tool name from a session event.
func getToolName(event copilot.SessionEvent) string {
	if event.Data.ToolName != nil {
		return *event.Data.ToolName
	}

	return "unknown"
}

// formatArgsMap converts a map of arguments to a comma-separated key=value string.
// Keys are sorted for consistent output across runs.
func formatArgsMap(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}

	// Sort keys for consistent output (Go map iteration order is non-deterministic)
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, args[k]))
	}

	return strings.Join(parts, ", ")
}

// getToolArgs formats tool arguments for display with parentheses.
func getToolArgs(event copilot.SessionEvent) string {
	if event.Data.Arguments == nil {
		return ""
	}

	args, ok := event.Data.Arguments.(map[string]any)
	if !ok || len(args) == 0 {
		return ""
	}

	formatted := formatArgsMap(args)
	if formatted == "" {
		return ""
	}

	return " (" + formatted + ")"
}

// formatToolArguments converts tool invocation arguments to a display string.
func formatToolArguments(args any) string {
	params, ok := args.(map[string]any)
	if !ok {
		return ""
	}

	return formatArgsMap(params)
}

// buildPlanModeBlockedResult creates a ToolResult indicating tool execution was blocked in plan mode.
func buildPlanModeBlockedResult(toolName string) (copilot.ToolResult, error) {
	cmdDescription := strings.ReplaceAll(toolName, "_", " ")

	return copilot.ToolResult{
		TextResultForLLM: "Tool execution blocked - currently in Plan mode.\n" +
			"Tool: " + cmdDescription + "\n" +
			"In Plan mode, I can only describe what I would do, not execute tools.\n" +
			"Switch to Agent mode (press Tab) to execute tools.",
		ResultType: "failure",
		SessionLog: "[PLAN MODE BLOCKED] " + cmdDescription,
		Error:      "Tool execution blocked in plan mode: " + toolName,
	}, nil
}

// awaitToolPermission sends a permission request to the TUI and waits for user response.
// Returns the approval state and an optional denial/timeout ToolResult.
func awaitToolPermission(
	eventChan chan tea.Msg,
	toolName string,
	invocation copilot.ToolInvocation,
) (bool, *copilot.ToolResult) {
	responseChan := make(chan bool, 1)
	cmdDescription := strings.ReplaceAll(toolName, "_", " ")

	eventChan <- chatui.PermissionRequestMsg{
		ToolCallID: invocation.ToolCallID,
		ToolName:   toolName,
		Command:    cmdDescription,
		Arguments:  formatToolArguments(invocation.Arguments),
		Response:   responseChan,
	}

	var approved bool

	select {
	case approved = <-responseChan:
	case <-time.After(permissionTimeoutMinutes * time.Minute):
		return false, &copilot.ToolResult{
			TextResultForLLM: "Permission request timed out for: " + cmdDescription + "\n" +
				"The user did not respond within the timeout period.",
			ResultType: "failure",
			SessionLog: "[TIMEOUT] " + cmdDescription,
		}
	}

	if !approved {
		return false, &copilot.ToolResult{
			TextResultForLLM: "Permission denied by user for: " + cmdDescription + "\n" +
				"The user chose not to allow this operation.",
			ResultType: "failure",
			SessionLog: "[DENIED] " + cmdDescription,
		}
	}

	return true, nil
}

// WrapToolsWithPermissionAndModeMetadata wraps ALL tools with mode enforcement and permission prompts.
// In plan mode, ALL tool execution is blocked (model can only describe what it would do).
// In agent mode, edit tools require permission (based on RequiresPermission annotation),
// while read-only tools are auto-approved.
func WrapToolsWithPermissionAndModeMetadata(
	tools []copilot.Tool,
	eventChan chan tea.Msg,
	agentModeRef *chatui.AgentModeRef,
	toolMetadata map[string]toolgen.ToolDefinition,
) []copilot.Tool {
	wrappedTools := make([]copilot.Tool, len(tools))

	for toolIdx, tool := range tools {
		wrappedTools[toolIdx] = tool

		// Create per-iteration copies to avoid closure capture bug.
		// Each handler must use its own tool's name and handler, not the last iteration's values.
		originalHandler := tool.Handler
		toolName := tool.Name

		wrappedTools[toolIdx].Handler = func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			if agentModeRef != nil && !agentModeRef.IsEnabled() {
				return buildPlanModeBlockedResult(toolName)
			}

			// Check if tool requires permission from metadata.
			// If metadata is nil or tool not found, defaults to requiresPermission=false (auto-approve).
			requiresPermission := false
			if metadata, ok := toolMetadata[toolName]; ok {
				requiresPermission = metadata.RequiresPermission
			}

			if !requiresPermission {
				return originalHandler(invocation)
			}

			approved, result := awaitToolPermission(eventChan, toolName, invocation)
			if !approved {
				return *result, nil
			}

			return originalHandler(invocation)
		}
	}

	return wrappedTools
}
