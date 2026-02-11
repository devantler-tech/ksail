package toolgen

import (
	"strings"

	"github.com/spf13/cobra"
)

// shouldConsolidate checks if a command should consolidate its subcommands.
func shouldConsolidate(cmd *cobra.Command) bool {
	if cmd.Annotations == nil {
		return false
	}

	_, hasAnnotation := cmd.Annotations[AnnotationConsolidate]

	return hasAnnotation && len(cmd.Commands()) > 0
}

// commandToPermissionSplitTools splits a parent command into read and write tools based on permission.
// It flattens all nested subcommands recursively and groups them by permission.
// If the parent has an explicit permission, all subcommands inherit it and create a single tool.
func commandToPermissionSplitTools(cmd *cobra.Command) []ToolDefinition {
	// Get the subcommand parameter name from annotation
	subcommandParam := cmd.Annotations[AnnotationConsolidate]

	// Build base name and path
	cmdPath := cmd.CommandPath()
	strippedPath := stripRootCommand(cmdPath)
	baseName := strings.ReplaceAll(strippedPath, " ", "_")

	// Check if parent has explicit permission - if so, don't split
	parentHasPermission := cmd.Annotations != nil &&
		cmd.Annotations[AnnotationPermission] != ""
	parentRequiresWrite := cmd.Annotations != nil &&
		cmd.Annotations[AnnotationPermission] == permissionWrite

	// If parent has explicit permission, create single tool without splitting
	if parentHasPermission {
		allSubcommands := make(map[string]*SubcommandDef)
		collectAllSubcommands(cmd, &allSubcommands)

		tool := buildConsolidatedTool(
			baseName,
			cmd,
			subcommandParam,
			allSubcommands,
			parentRequiresWrite,
		)

		return []ToolDefinition{tool}
	}

	// Parent has no explicit permission - split by children's permissions
	readSubcommands := make(map[string]*SubcommandDef)
	writeSubcommands := make(map[string]*SubcommandDef)

	collectSubcommandsRecursively(cmd, &readSubcommands, &writeSubcommands, false)

	// Build tool definitions
	var tools []ToolDefinition

	// Create read tool if there are read subcommands
	if len(readSubcommands) > 0 {
		readTool := buildConsolidatedTool(
			baseName+"_read",
			cmd,
			subcommandParam,
			readSubcommands,
			false, // read-only, no permission required
		)
		tools = append(tools, readTool)
	}

	// Create write tool if there are write subcommands
	if len(writeSubcommands) > 0 {
		writeTool := buildConsolidatedTool(
			baseName+"_write",
			cmd,
			subcommandParam,
			writeSubcommands,
			true, // write requires permission
		)
		tools = append(tools, writeTool)
	}

	return tools
}

// collectAllSubcommands collects all subcommands recursively without regard to permission.
// Uses relative path from parent as map key to avoid naming collisions.
func collectAllSubcommands(parent *cobra.Command, subcommands *map[string]*SubcommandDef) {
	collectAllSubcommandsWithPrefix(parent, subcommands, "")
}

// collectAllSubcommandsWithPrefix recursively collects subcommands with a path prefix.
func collectAllSubcommandsWithPrefix(
	cmd *cobra.Command,
	subcommands *map[string]*SubcommandDef,
	prefix string,
) {
	for _, subCmd := range cmd.Commands() {
		// Skip hidden subcommands
		if subCmd.Hidden {
			continue
		}

		// Build the relative key: prefix_name or just name if no prefix
		relativeKey := subCmd.Name()
		if prefix != "" {
			relativeKey = prefix + "_" + subCmd.Name()
		}

		subCmdPath := subCmd.CommandPath()
		subCmdParts := strings.Fields(subCmdPath)
		flags := extractFlags(subCmd)

		// If this subcommand has its own children, check if it's also runnable
		if len(subCmd.Commands()) > 0 {
			// Include runnable parent commands (have RunE and non-help flags)
			if isRunnableCommand(subCmd) {
				(*subcommands)[relativeKey] = &SubcommandDef{
					Name:         relativeKey,
					Description:  subCmd.Short,
					CommandParts: subCmdParts,
					Flags:        flags,
				}
			}
			// Recursively collect nested subcommands with updated prefix
			collectAllSubcommandsWithPrefix(subCmd, subcommands, relativeKey)
		} else {
			// Leaf command - add it to the map
			(*subcommands)[relativeKey] = &SubcommandDef{
				Name:         relativeKey,
				Description:  subCmd.Short,
				CommandParts: subCmdParts,
				Flags:        flags,
			}
		}
	}
}

// collectSubcommandsRecursively recursively collects all subcommands and their nested children,
// splitting them by permission into read and write maps.
// Uses relative path from parent as map key to avoid naming collisions.
func collectSubcommandsRecursively(
	parent *cobra.Command,
	readSubcommands *map[string]*SubcommandDef,
	writeSubcommands *map[string]*SubcommandDef,
	parentRequiresWrite bool,
) {
	collectSubcommandsWithPrefix(parent, readSubcommands, writeSubcommands, "", parentRequiresWrite)
}

// collectSubcommandsWithPrefix recursively collects subcommands with permission splitting and path prefix.
func collectSubcommandsWithPrefix(
	cmd *cobra.Command,
	readSubcommands *map[string]*SubcommandDef,
	writeSubcommands *map[string]*SubcommandDef,
	prefix string,
	parentRequiresWrite bool,
) {
	for _, subCmd := range cmd.Commands() {
		// Skip hidden subcommands
		if subCmd.Hidden {
			continue
		}

		// Build the relative key: prefix_name or just name if no prefix
		relativeKey := buildRelativeKey(prefix, subCmd.Name())

		// Determine if this command requires write permission
		requiresWrite := determineWritePermission(subCmd, parentRequiresWrite)

		subcommandDef := buildSubcommandDef(subCmd, relativeKey)

		// Process subcommand based on whether it has children
		processSubcommand(
			subCmd,
			subcommandDef,
			relativeKey,
			requiresWrite,
			readSubcommands,
			writeSubcommands,
		)
	}
}

// buildRelativeKey constructs the relative key for a subcommand.
func buildRelativeKey(prefix, name string) string {
	if prefix != "" {
		return prefix + "_" + name
	}

	return name
}

// determineWritePermission checks if a command requires write permission.
func determineWritePermission(cmd *cobra.Command, parentRequiresWrite bool) bool {
	if cmd.Annotations != nil && cmd.Annotations[AnnotationPermission] != "" {
		return cmd.Annotations[AnnotationPermission] == permissionWrite
	}

	return parentRequiresWrite
}

// buildSubcommandDef creates a SubcommandDef from a command.
func buildSubcommandDef(cmd *cobra.Command, relativeKey string) *SubcommandDef {
	return &SubcommandDef{
		Name:         relativeKey,
		Description:  cmd.Short,
		CommandParts: strings.Fields(cmd.CommandPath()),
		Flags:        extractFlags(cmd),
	}
}

// processSubcommand processes a subcommand with its children.
func processSubcommand(
	subCmd *cobra.Command,
	subcommandDef *SubcommandDef,
	relativeKey string,
	requiresWrite bool,
	readSubcommands *map[string]*SubcommandDef,
	writeSubcommands *map[string]*SubcommandDef,
) {
	hasChildren := len(subCmd.Commands()) > 0

	// Add to appropriate map if runnable (or if it's a leaf command)
	if !hasChildren || isRunnableCommand(subCmd) {
		addToSubcommandMap(
			subcommandDef,
			relativeKey,
			requiresWrite,
			readSubcommands,
			writeSubcommands,
		)
	}

	// Recursively collect nested subcommands
	if hasChildren {
		collectSubcommandsWithPrefix(
			subCmd,
			readSubcommands,
			writeSubcommands,
			relativeKey,
			requiresWrite,
		)
	}
}

// addToSubcommandMap adds a subcommand definition to the appropriate map based on permission.
func addToSubcommandMap(
	def *SubcommandDef,
	key string,
	requiresWrite bool,
	readSubcommands *map[string]*SubcommandDef,
	writeSubcommands *map[string]*SubcommandDef,
) {
	if requiresWrite {
		(*writeSubcommands)[key] = def
	} else {
		(*readSubcommands)[key] = def
	}
}

// buildConsolidatedTool creates a consolidated tool definition with the given parameters.
func buildConsolidatedTool(
	toolName string,
	cmd *cobra.Command,
	subcommandParam string,
	subcommands map[string]*SubcommandDef,
	requiresPermission bool,
) ToolDefinition {
	// Build description
	description := cmd.Short
	if cmd.Annotations != nil && cmd.Annotations[AnnotationDescription] != "" {
		description = cmd.Annotations[AnnotationDescription]
	}

	// Build dynamic parameter schema
	parameters := buildConsolidatedParameterSchema(subcommandParam, subcommands)

	cmdPath := cmd.CommandPath()
	cmdParts := strings.Fields(cmdPath)

	return ToolDefinition{
		Name:               toolName,
		Description:        description,
		Parameters:         parameters,
		CommandPath:        cmdPath,
		CommandParts:       cmdParts,
		RequiresPermission: requiresPermission,
		IsConsolidated:     true,
		SubcommandParam:    subcommandParam,
		Subcommands:        subcommands,
	}
}
