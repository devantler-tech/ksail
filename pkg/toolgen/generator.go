package toolgen

import (
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/strutil"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GenerateTools traverses a Cobra command tree and generates tool definitions.
// It returns SDK-agnostic tool definitions for all runnable leaf commands.
func GenerateTools(root *cobra.Command, opts ToolOptions) []ToolDefinition {
	var tools []ToolDefinition
	generateToolsRecursive(root, &tools, opts)

	return tools
}

// generateToolsRecursive traverses the command tree depth-first.
func generateToolsRecursive(cmd *cobra.Command, tools *[]ToolDefinition, opts ToolOptions) {
	// Skip entirely (including children) when the command is excluded or marked
	// interactive. isExcludedFromTools is the single policy shared with the
	// consolidation collector (walkSubcommands).
	if isExcludedFromTools(cmd) {
		return
	}

	// Check if this command (and its children) should be excluded via prefix match
	// Prefix matching only applies to multi-word exclusions like "ksail completion"
	if shouldExcludeWithChildren(cmd, opts) {
		return
	}

	// Check if this command itself is excluded (but children may not be)
	isExcluded := shouldExclude(cmd, opts)

	// Check if this command should be consolidated (only if not excluded)
	if !isExcluded && shouldConsolidate(cmd) {
		splitTools := commandToPermissionSplitTools(cmd, opts.ExcludeFlags)
		*tools = append(*tools, splitTools...)

		return // Don't traverse children - they're now part of the consolidated tool(s)
	}

	// Traverse children and potentially add this command as a tool
	processCommandAndChildren(cmd, tools, opts, isExcluded)
}

// processCommandAndChildren traverses children and adds the command as a tool if applicable.
func processCommandAndChildren(
	cmd *cobra.Command,
	tools *[]ToolDefinition,
	opts ToolOptions,
	isExcluded bool,
) {
	// If command has subcommands, traverse them
	if len(cmd.Commands()) > 0 {
		for _, subCmd := range cmd.Commands() {
			generateToolsRecursive(subCmd, tools, opts)
		}
		// Skip generating tool for excluded or non-runnable parent commands
		if isExcluded || !isRunnableCommand(cmd) {
			return
		}
	}

	// Generate tool for runnable commands (if not excluded)
	if !isExcluded && isRunnableCommand(cmd) {
		tool := commandToToolDefinition(cmd, opts.ExcludeFlags)
		*tools = append(*tools, tool)
	}
}

// shouldExcludeWithChildren checks if a command and all its children should be excluded.
// This uses prefix matching for multi-word exclusions (e.g., "ksail completion")
// so excluding "ksail completion" also excludes "ksail completion bash".
func shouldExcludeWithChildren(cmd *cobra.Command, opts ToolOptions) bool {
	cmdPath := cmd.CommandPath()
	for _, excluded := range opts.ExcludeCommands {
		// Only apply prefix matching for subcommand paths (those containing spaces)
		// This means excluding "ksail completion" will also exclude its children
		if strings.Contains(excluded, " ") && strings.HasPrefix(cmdPath, excluded+" ") {
			return true
		}
	}

	return false
}

// shouldExclude checks if a command should be excluded from tool generation.
// This checks for exact matches only. Children are still processed.
func shouldExclude(cmd *cobra.Command, opts ToolOptions) bool {
	// Check hidden commands
	if cmd.Hidden && !opts.IncludeHidden {
		return true
	}

	// Check exclusion list for exact match
	cmdPath := cmd.CommandPath()

	return slices.Contains(opts.ExcludeCommands, cmdPath)
}

// isRunnableCommand checks if a command can actually be executed.
// Commands that only display help are not considered runnable.
func isRunnableCommand(cmd *cobra.Command) bool {
	// Must have either Run or RunE
	if cmd.Run == nil && cmd.RunE == nil {
		return false
	}

	// Skip commands that just show help (common pattern for group commands)
	// We detect this by checking if the command has subcommands and its RunE
	// just calls Help()
	if len(cmd.Commands()) > 0 && cmd.RunE != nil {
		// This is a heuristic - group commands typically only call Help()
		// We'll include it if it has flags beyond the standard help flag
		hasNonHelpFlags := false

		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name != helpFlagName {
				hasNonHelpFlags = true
			}
		})

		if !hasNonHelpFlags {
			return false
		}
	}

	return true
}

// stripRootCommand removes the root command from a command path.
// Example: "ksail cluster create" -> "cluster create"
// If only root command, returns it unchanged: "ksail" -> "ksail".
func stripRootCommand(commandPath string) string {
	parts := strings.Fields(commandPath)
	if len(parts) <= 1 {
		return commandPath
	}

	return strings.Join(parts[1:], " ")
}

// commandToToolDefinition converts a Cobra command to a tool definition.
func commandToToolDefinition(cmd *cobra.Command, excludeFlags []string) ToolDefinition {
	// Build tool name: "ksail cluster create" -> "cluster_create"
	cmdPath := cmd.CommandPath()
	strippedPath := stripRootCommand(cmdPath)
	toolName := strings.ReplaceAll(strippedPath, " ", "_")

	// Get description from annotation or Short
	description := cmd.Short
	if cmd.Annotations != nil && cmd.Annotations[annotations.AnnotationDescription] != "" {
		description = cmd.Annotations[annotations.AnnotationDescription]
	}

	// Build JSON schema from flags
	parameters := buildParameterSchema(cmd, excludeFlags)

	// Check if permission is required
	requiresPermission := cmd.Annotations != nil &&
		cmd.Annotations[annotations.AnnotationPermission] == permissionWrite

	// Split command path into parts
	cmdParts := strings.Fields(cmdPath)

	return ToolDefinition{
		Name:               toolName,
		Title:              buildToolTitle(strippedPath),
		Description:        description,
		Parameters:         parameters,
		CommandPath:        cmdPath,
		CommandParts:       cmdParts,
		RequiresPermission: requiresPermission,
		Annotations:        buildAnnotationHints(requiresPermission),
		ConfirmFlags:       confirmFlagNames(cmd),
	}
}

// buildToolTitle generates a human-readable title from a tool path.
// Example: "cluster create" -> "Cluster Create", "workload_read" -> "Workload Read".
func buildToolTitle(strippedPath string) string {
	return strutil.SnakeCaseToTitle(strippedPath)
}

// buildAnnotationHints creates behavioral hints based on permission level.
// Read-only tools are marked safe; write tools are marked destructive and non-idempotent.
// All KSail tools set openWorldHint=true: they can target remote clusters,
// cloud providers (Hetzner, AWS EKS), and OCI registries.
func buildAnnotationHints(requiresPermission bool) ToolAnnotationHints {
	if !requiresPermission {
		return ToolAnnotationHints{
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   true,
		}
	}

	return ToolAnnotationHints{
		ReadOnlyHint:    false,
		DestructiveHint: true,
		IdempotentHint:  false,
		OpenWorldHint:   true,
	}
}
