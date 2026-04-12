//nolint:testpackage // Test needs package access for internal helpers.
package chat

import (
	"github.com/devantler-tech/ksail/v6/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
)

// GetIsExitCommand returns the isExitCommand function for testing.
func GetIsExitCommand() func(string) bool {
	return isExitCommand
}

// GetGetToolName returns the getToolName function for testing.
func GetGetToolName() func(copilot.SessionEvent) string {
	return getToolName
}

// GetFormatArgsMap returns the formatArgsMap function for testing.
func GetFormatArgsMap() func(map[string]any) string {
	return formatArgsMap
}

// GetGetToolArgs returns the getToolArgs function for testing.
func GetGetToolArgs() func(copilot.SessionEvent) string {
	return getToolArgs
}

// GetInjectForceFlag returns the injectForceFlag function for testing.
func GetInjectForceFlag() func(copilot.ToolInvocation) copilot.ToolInvocation {
	return injectForceFlag
}

// GetToolSupportsForce returns the toolSupportsForce function for testing.
func GetToolSupportsForce() func(map[string]toolgen.ToolDefinition, string) bool {
	return toolSupportsForce
}

// GetValidatePathAccess returns the validatePathAccess function for testing.
func GetValidatePathAccess() func(copilot.PreToolUseHookInput, string) (*copilot.PreToolUseHookOutput, error) {
	return validatePathAccess
}
