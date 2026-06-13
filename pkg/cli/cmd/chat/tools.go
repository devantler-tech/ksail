package chat

import (
	"fmt"

	chatsvc "github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
)

// injectConfirmFlags sets each confirmation-skip flag to true in the
// invocation arguments. Only flags carrying the ai.toolgen.confirm-flag
// annotation reach here, so flags with other semantics (kubectl's destructive
// --force, init's force=overwrite) are never injected.
func injectConfirmFlags(
	invocation copilot.ToolInvocation,
	flagNames []string,
) copilot.ToolInvocation {
	if len(flagNames) == 0 {
		return invocation
	}

	args, ok := invocation.Arguments.(map[string]any)
	if !ok || args == nil {
		args = map[string]any{}
	}

	for _, name := range flagNames {
		args[name] = true
	}

	invocation.Arguments = args

	return invocation
}

// confirmFlagsForInvocation resolves which confirmation-skip flags apply to a
// tool invocation: flags carrying the ai.toolgen.confirm-flag annotation,
// resolved per-subcommand for consolidated tools (e.g. cluster_write
// command="delete" yields force, while command="init" yields none).
func confirmFlagsForInvocation(
	metadata map[string]toolgen.ToolDefinition,
	toolName string,
	invocation copilot.ToolInvocation,
) []string {
	meta, metaExists := metadata[toolName]
	if !metaExists {
		return nil
	}

	args, _ := invocation.Arguments.(map[string]any)

	return meta.ConfirmFlagsFor(args)
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
		return nil, nil //nolint:nilnil // nil omits "output" key from JSON-RPC response
	}

	args, ok := input.ToolArgs.(map[string]any)
	if !ok || len(args) == 0 {
		return nil, nil //nolint:nilnil // nil omits "output" key from JSON-RPC response
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

	return nil, nil //nolint:nilnil // nil omits "output" key from JSON-RPC response
}

// WrapToolsWithForceInjection wraps tools to inject confirmation-skip flags
// (flags annotated with ai.toolgen.confirm-flag, e.g. cluster update/delete
// --force) after SDK-native permission approval, so approved write operations
// don't block on KSail's own interactive prompts. Permission handling is
// delegated entirely to the SDK's OnPermissionRequest handler — this wrapper
// only handles confirm-flag injection, resolved per-subcommand for
// consolidated tools.
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

// invokeWithOptionalForce injects the applicable confirmation-skip flags
// (if any), then calls the handler.
func invokeWithOptionalForce(
	invocation copilot.ToolInvocation,
	toolMetadata map[string]toolgen.ToolDefinition,
	toolName string,
	handler func(copilot.ToolInvocation) (copilot.ToolResult, error),
) (copilot.ToolResult, error) {
	confirmFlags := confirmFlagsForInvocation(toolMetadata, toolName, invocation)

	return handler(injectConfirmFlags(invocation, confirmFlags))
}
