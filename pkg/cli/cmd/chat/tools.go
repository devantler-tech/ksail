package chat

import (
	"fmt"
	"slices"
	"strings"

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
// the allowed root directory. Returns nil for valid paths so the SDK sends no hook output
// to the CLI server (identical to having no hook registered), avoiding a duplicate
// permission.request. Returns "deny" only when a path escapes the allowed root.
func validatePathAccess(
	input copilot.PreToolUseHookInput,
	allowedRoot string,
) (*copilot.PreToolUseHookOutput, error) {
	if allowedRoot == "" {
		//nolint:nilnil // nil output omits the "output" key from the RPC response, matching no-hook behavior
		return nil, nil
	}

	args, ok := input.ToolArgs.(map[string]any)
	if !ok || len(args) == 0 {
		//nolint:nilnil // nil output omits the "output" key from the RPC response, matching no-hook behavior
		return nil, nil
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

	//nolint:nilnil // nil output omits the "output" key from the RPC response, matching no-hook behavior
	return nil, nil
}

// WrapToolsWithForceInjection wraps write tools to inject the --force flag after
// SDK-native permission approval. Permission handling is delegated entirely to the
// SDK's OnPermissionRequest handler — this wrapper only handles force-flag injection.
func WrapToolsWithForceInjection(
	tools []copilot.Tool,
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
			return invokeWithOptionalForce(invocation, toolMetadata, toolName, originalHandler)
		}
	}

	return wrappedTools
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
