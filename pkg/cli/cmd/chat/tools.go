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

// injectForceFlag injects a "force" argument into the tool invocation.
// This skips interactive confirmation prompts when the tool supports --force.
// Only call this after verifying the tool supports force via toolSupportsForce.
func injectForceFlag(invocation copilot.ToolInvocation) copilot.ToolInvocation {
	args, ok := invocation.Arguments.(map[string]any)
	if !ok || args == nil {
		args = map[string]any{}
	}

	args["force"] = true
	invocation.Arguments = args

	return invocation
}

// toolSupportsForce reports whether the tool's parameter schema defines a "force" property.
// This prevents injecting --force into tools that don't accept it, which would cause
// runtime failures for non-consolidated tools that pass all parameters as CLI flags.
func toolSupportsForce(metadata map[string]toolgen.ToolDefinition, toolName string) bool {
	if metadata == nil {
		return false
	}

	meta, metaExists := metadata[toolName]
	if !metaExists || meta.Parameters == nil {
		return false
	}

	propertiesVal, propsExists := meta.Parameters["properties"]
	if !propsExists {
		return false
	}

	properties, propsIsMap := propertiesVal.(map[string]any)
	if !propsIsMap {
		return false
	}

	_, hasForce := properties["force"]

	return hasForce
}

// buildPlanModeBlockedResult creates a ToolResult indicating tool execution was blocked in plan mode.
func buildPlanModeBlockedResult(toolName string) copilot.ToolResult {
	cmdDescription := strings.ReplaceAll(toolName, "_", " ")

	return copilot.ToolResult{
		TextResultForLLM: "Tool execution blocked - currently in Plan mode.\n" +
			"Tool: " + cmdDescription + "\n" +
			"In Plan mode, I can only describe what I would do, not execute tools.\n" +
			"Switch to Agent mode (press Tab) to execute tools.",
		ResultType: "failure",
		SessionLog: "[PLAN MODE BLOCKED] " + cmdDescription,
		Error:      "Tool execution blocked in plan mode: " + toolName,
	}
}

// buildAskModeBlockedResult creates a ToolResult indicating a write tool was blocked in ask mode.
func buildAskModeBlockedResult(toolName string) copilot.ToolResult {
	cmdDescription := strings.ReplaceAll(toolName, "_", " ")

	return copilot.ToolResult{
		TextResultForLLM: "Write tool blocked - currently in Ask mode (read-only).\n" +
			"Tool: " + cmdDescription + "\n" +
			"In Ask mode, only read-only tools can execute. Write operations are blocked.\n" +
			"Switch to Agent mode (press Tab) to execute write tools.",
		ResultType: "failure",
		SessionLog: "[ASK MODE BLOCKED] " + cmdDescription,
		Error:      "Write tool blocked in ask mode: " + toolName,
	}
}

// awaitToolPermission sends a permission request to the TUI and waits for user response.
// Returns the approval state and an optional denial/timeout ToolResult.
func awaitToolPermission(
	eventChan chan<- tea.Msg,
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

// BuildPreToolUseHook creates a PreToolUseHandler that enforces chat mode restrictions on ALL tool
// invocations (both custom KSail tools and SDK-managed tools like git/shell/filesystem).
// In plan mode, ALL tools are blocked. In ask mode, write tools and unknown tools are blocked.
// In agent mode, the hook defers to existing permission mechanisms (Handler wrappers + OnPermissionRequest).
func BuildPreToolUseHook(
	chatModeRef *chatui.ChatModeRef,
	toolMetadata map[string]toolgen.ToolDefinition,
) copilot.PreToolUseHandler {
	return func(input copilot.PreToolUseHookInput, _ copilot.HookInvocation) (*copilot.PreToolUseHookOutput, error) {
		if chatModeRef == nil {
			return &copilot.PreToolUseHookOutput{}, nil
		}

		switch chatModeRef.Mode() {
		case chatui.PlanMode:
			return &copilot.PreToolUseHookOutput{
				PermissionDecision: "deny",
				PermissionDecisionReason: "Tool execution blocked — currently in Plan mode. " +
					"Switch to Agent mode (press Tab) to execute tools.",
			}, nil
		case chatui.AskMode:
			return handleAskModeToolUse(input.ToolName, toolMetadata)
		case chatui.AgentMode:
			// Defer to existing handler wrappers and OnPermissionRequest
			return &copilot.PreToolUseHookOutput{}, nil
		}

		return &copilot.PreToolUseHookOutput{}, nil
	}
}

// handleAskModeToolUse determines if a tool should be allowed or denied in ask mode.
// Known read-only tools (RequiresPermission=false) are allowed; write tools and unknown
// (SDK-managed) tools are denied for safety.
func handleAskModeToolUse(
	toolName string,
	toolMetadata map[string]toolgen.ToolDefinition,
) (*copilot.PreToolUseHookOutput, error) {
	if metadata, exists := toolMetadata[toolName]; exists {
		if !metadata.RequiresPermission {
			// Known read-only tool — allow
			return &copilot.PreToolUseHookOutput{}, nil
		}
	}

	// Write tool or unknown (SDK-managed) tool — deny in ask mode
	return &copilot.PreToolUseHookOutput{
		PermissionDecision: "deny",
		PermissionDecisionReason: "Write tool blocked — currently in Ask mode (read-only). " +
			"Switch to Agent mode (press Tab) to execute write tools.",
	}, nil
}

// WrapToolsWithPermissionAndModeMetadata wraps ALL tools with mode enforcement and permission prompts.
// In plan mode, ALL tool execution is blocked (model can only describe what it would do).
// In ask mode, write tools are blocked but read-only tools execute normally.
// In agent mode, edit tools require permission (based on RequiresPermission annotation),
// while read-only tools are auto-approved.
// When YOLO mode is enabled, permission prompts are skipped and all tools are auto-approved.
func WrapToolsWithPermissionAndModeMetadata(
	tools []copilot.Tool,
	eventChan chan tea.Msg,
	chatModeRef *chatui.ChatModeRef,
	yoloModeRef *chatui.YoloModeRef,
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
			return executeWrappedTool(
				invocation, toolName, originalHandler,
				eventChan, chatModeRef, yoloModeRef, toolMetadata,
			)
		}
	}

	return wrappedTools
}

// executeModeCheck checks the current chat mode and returns a result when the call
// has been fully handled — either by blocking the tool or by executing it directly
// (e.g. read-only tools in AskMode). Returns (result, true) if handled, (zero, false)
// if execution should proceed through the normal agent-mode path.
func executeModeCheck(
	chatModeRef *chatui.ChatModeRef,
	toolName string,
	invocation copilot.ToolInvocation,
	originalHandler func(copilot.ToolInvocation) (copilot.ToolResult, error),
	toolMetadata map[string]toolgen.ToolDefinition,
) (copilot.ToolResult, bool, error) {
	if chatModeRef == nil {
		return copilot.ToolResult{}, false, nil
	}

	switch chatModeRef.Mode() {
	case chatui.PlanMode:
		result := buildPlanModeBlockedResult(toolName)

		return result, true, nil
	case chatui.AskMode:
		// In ask mode, only execute tools that are explicitly known and do not require permission.
		// Tools without metadata or that require permission are blocked by default.
		metadata, metaExists := toolMetadata[toolName]
		if !metaExists || metadata.RequiresPermission {
			result := buildAskModeBlockedResult(toolName)

			return result, true, nil
		}

		result, err := originalHandler(invocation)

		return result, true, err
	case chatui.AgentMode:
		// Fall through to normal agent mode logic
	}

	return copilot.ToolResult{}, false, nil
}

// executeWrappedTool handles mode enforcement, permission checks, YOLO mode,
// force flag injection, and user approval for a single tool invocation.
func executeWrappedTool(
	invocation copilot.ToolInvocation,
	toolName string,
	originalHandler func(copilot.ToolInvocation) (copilot.ToolResult, error),
	eventChan chan tea.Msg,
	chatModeRef *chatui.ChatModeRef,
	yoloModeRef *chatui.YoloModeRef,
	toolMetadata map[string]toolgen.ToolDefinition,
) (copilot.ToolResult, error) {
	// Check chat mode for tool blocking
	if result, handled, err := executeModeCheck(
		chatModeRef, toolName, invocation, originalHandler, toolMetadata,
	); handled {
		return result, err
	}

	// Agent mode: check if tool requires permission from metadata.
	// If metadata is nil or tool not found, defaults to requiresPermission=false (auto-approve).
	requiresPermission := false
	if metadata, metaExists := toolMetadata[toolName]; metaExists {
		requiresPermission = metadata.RequiresPermission
	}

	if !requiresPermission {
		return originalHandler(invocation)
	}

	// In YOLO mode, auto-approve all tools that would normally require permission
	if yoloModeRef != nil && yoloModeRef.IsEnabled() {
		return invokeWithOptionalForce(invocation, toolMetadata, toolName, originalHandler)
	}

	// If no event channel is available (non-TUI mode), auto-approve write tools
	// to avoid deadlocking on a nil channel send.
	if eventChan == nil {
		return invokeWithOptionalForce(invocation, toolMetadata, toolName, originalHandler)
	}

	approved, result := awaitToolPermission(eventChan, toolName, invocation)
	if !approved {
		return *result, nil
	}

	return invokeWithOptionalForce(invocation, toolMetadata, toolName, originalHandler)
}

// invokeWithOptionalForce injects the force flag if the tool supports it, then calls the handler.
func invokeWithOptionalForce(
	invocation copilot.ToolInvocation,
	toolMetadata map[string]toolgen.ToolDefinition,
	toolName string,
	handler func(copilot.ToolInvocation) (copilot.ToolResult, error),
) (copilot.ToolResult, error) {
	if toolSupportsForce(toolMetadata, toolName) {
		invocation = injectForceFlag(invocation)
	}

	return handler(invocation)
}
