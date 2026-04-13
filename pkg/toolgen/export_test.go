//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package toolgen

// BuildMCPSuccessText exposes buildMCPSuccessText for testing.
var BuildMCPSuccessText = buildMCPSuccessText

// BuildMCPErrorText exposes buildMCPErrorText for testing.
var BuildMCPErrorText = buildMCPErrorText

// BuildFullCommand exposes buildFullCommand for testing.
var BuildFullCommand = buildFullCommand

// BuildCopilotResult exposes buildCopilotResult for testing.
var BuildCopilotResult = buildCopilotResult

// FormatParametersForDisplay exposes formatParametersForDisplay for testing.
var FormatParametersForDisplay = formatParametersForDisplay

// FormatPositionalArgs exposes formatPositionalArgs for testing.
var FormatPositionalArgs = formatPositionalArgs

// CollectAllSubcommands exposes collectAllSubcommands for testing.
var CollectAllSubcommands = collectAllSubcommands

// CollectAllSubcommandsWithPrefix exposes collectAllSubcommandsWithPrefix for testing.
var CollectAllSubcommandsWithPrefix = collectAllSubcommandsWithPrefix

// ExecuteTool exposes executeTool for testing.
var ExecuteTool = executeTool
