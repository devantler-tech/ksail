package chat

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	chatsvc "github.com/devantler-tech/ksail/v5/pkg/svc/chat"
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

// pathArgKeys returns the argument keys that SDK-managed file tools use for paths.
// Checked in order; the first match is validated.
func pathArgKeys() []string {
	return []string{"path", "filePath", "file", "target", "directory"}
}

// BuildPreToolUseHook creates a PreToolUseHandler that enforces path sandboxing on ALL tool
// invocations (both custom KSail tools and SDK-managed tools like git/shell/filesystem).
// Mode enforcement (plan mode tool blocking) is handled server-side via Session.RPC.Mode.Set().
func BuildPreToolUseHook(
	allowedRoot string,
) copilot.PreToolUseHandler {
	return func(input copilot.PreToolUseHookInput, _ copilot.HookInvocation) (*copilot.PreToolUseHookOutput, error) {
		return validatePathAccess(input, allowedRoot)
	}
}

// validatePathAccess checks whether a tool invocation's file path arguments fall within
// the allowed root directory. Only SDK-managed tools (not in toolMetadata) are checked.
func validatePathAccess(
	input copilot.PreToolUseHookInput,
	allowedRoot string,
) (*copilot.PreToolUseHookOutput, error) {
	if allowedRoot == "" {
		return &copilot.PreToolUseHookOutput{}, nil
	}

	args, ok := input.ToolArgs.(map[string]any)
	if !ok || len(args) == 0 {
		return &copilot.PreToolUseHookOutput{}, nil
	}

	for _, key := range pathArgKeys() {
		val, exists := args[key]
		if !exists {
			continue
		}

		pathStr, isStr := val.(string)
		if !isStr || pathStr == "" {
			continue
		}

		if !chatsvc.IsPathWithinDirectory(pathStr, allowedRoot) {
			return &copilot.PreToolUseHookOutput{
				PermissionDecision: "deny",
				PermissionDecisionReason: fmt.Sprintf(
					"Access denied — path %q is outside the project directory (%s). "+
						"File access is restricted to the current working directory and its subdirectories.",
					pathStr, allowedRoot,
				),
			}, nil
		}
	}

	return &copilot.PreToolUseHookOutput{}, nil
}

// WrapToolsWithPermissionAndModeMetadata wraps ALL tools with permission prompts.
// In agent mode, edit tools require permission (based on RequiresPermission annotation),
// while read-only tools are auto-approved.
// When YOLO mode is enabled, permission prompts are skipped and all tools are auto-approved.
// Mode enforcement (plan mode tool blocking) is handled server-side via Session.RPC.Mode.Set().
func WrapToolsWithPermissionAndModeMetadata(
	tools []copilot.Tool,
	eventChan chan tea.Msg,
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
				eventChan, yoloModeRef, toolMetadata,
			)
		}
	}

	return wrappedTools
}

// executeWrappedTool handles permission checks, YOLO mode,
// force flag injection, and user approval for a single tool invocation.
// Mode enforcement (plan mode) is handled server-side via Session.RPC.Mode.Set().
func executeWrappedTool(
	invocation copilot.ToolInvocation,
	toolName string,
	originalHandler func(copilot.ToolInvocation) (copilot.ToolResult, error),
	eventChan chan tea.Msg,
	yoloModeRef *chatui.YoloModeRef,
	toolMetadata map[string]toolgen.ToolDefinition,
) (copilot.ToolResult, error) {
	// Check if tool requires permission from metadata.
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
