package toolgen

import (
	"fmt"
	"slices"
	"strings"

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
	// Skip excluded commands
	if shouldExclude(cmd, opts) {
		return
	}

	// Check for explicit exclusion annotation
	if cmd.Annotations != nil && cmd.Annotations[AnnotationExclude] == "true" {
		return
	}

	// Check if this command should be consolidated
	if shouldConsolidate(cmd) {
		splitTools := commandToPermissionSplitTools(cmd)
		*tools = append(*tools, splitTools...)

		return // Don't traverse children - they're now part of the consolidated tool(s)
	}

	// If command has subcommands, traverse them
	if len(cmd.Commands()) > 0 {
		for _, subCmd := range cmd.Commands() {
			generateToolsRecursive(subCmd, tools, opts)
		}
		// Also check if this command is runnable itself (has RunE and isn't just a group)
		if !isRunnableCommand(cmd) {
			return
		}
	}

	// Generate tool for runnable commands
	if isRunnableCommand(cmd) {
		tool := commandToToolDefinition(cmd)
		*tools = append(*tools, tool)
	}
}

// shouldExclude checks if a command should be excluded from tool generation.
func shouldExclude(cmd *cobra.Command, opts ToolOptions) bool {
	// Check hidden commands
	if cmd.Hidden && !opts.IncludeHidden {
		return true
	}

	// Check exclusion list
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
			if f.Name != flagTypeHelp {
				hasNonHelpFlags = true
			}
		})

		if !hasNonHelpFlags {
			return false
		}
	}

	return true
}

// commandToToolDefinition converts a Cobra command to a tool definition.
func commandToToolDefinition(cmd *cobra.Command) ToolDefinition {
	// Build tool name: "ksail cluster create" -> "ksail_cluster_create"
	cmdPath := cmd.CommandPath()
	toolName := strings.ReplaceAll(cmdPath, " ", "_")

	// Get description from annotation or Short
	description := cmd.Short
	if cmd.Annotations != nil && cmd.Annotations[AnnotationDescription] != "" {
		description = cmd.Annotations[AnnotationDescription]
	}
	// Append Long description if available and different
	if cmd.Long != "" && cmd.Long != cmd.Short {
		description = description + "\n\n" + cmd.Long
	}

	// Build JSON schema from flags
	parameters := buildParameterSchema(cmd)

	// Check if permission is required
	requiresPermission := cmd.Annotations != nil &&
		cmd.Annotations[AnnotationPermission] == permissionWrite

	// Split command path into parts
	cmdParts := strings.Fields(cmdPath)

	return ToolDefinition{
		Name:               toolName,
		Description:        description,
		Parameters:         parameters,
		CommandPath:        cmdPath,
		CommandParts:       cmdParts,
		RequiresPermission: requiresPermission,
	}
}

// buildParameterSchema creates a JSON schema from Cobra command flags.
func buildParameterSchema(cmd *cobra.Command) map[string]any {
	properties := make(map[string]any)
	required := []string{}

	// Visit all flags (local and persistent)
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		// Skip help flag
		if flag.Name == flagTypeHelp {
			return
		}

		prop := flagToSchemaProperty(flag)
		properties[flag.Name] = prop

		// Mark as required if no default value and not a bool
		// Bools default to false so they're never truly "required"
		if flag.DefValue == "" && flag.Value.Type() != flagTypeBool {
			required = append(required, flag.Name)
		}
	})

	// Check for positional arguments
	if cmd.Args != nil {
		// Add positional args parameter for commands that expect them
		// We'll use a generic "args" parameter
		properties["args"] = map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Positional arguments for the command",
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// flagToSchemaProperty converts a pflag to a JSON schema property.
func flagToSchemaProperty(flag *pflag.Flag) map[string]any {
	// Check if the flag value implements enumValuer interface
	if ev, ok := flag.Value.(enumValuer); ok {
		if enumProp := buildEnumProperty(ev, flag); enumProp != nil {
			return enumProp
		}
	}

	// Build standard property
	prop := map[string]any{
		"description": flag.Usage,
	}

	// Map pflag types to JSON schema types
	switch flag.Value.Type() {
	case flagTypeBool:
		prop["type"] = jsonSchemaTypeBoolean
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		prop["type"] = jsonSchemaTypeInteger
	case "float32", "float64":
		prop["type"] = jsonSchemaTypeNumber
	case "stringSlice", "stringArray":
		prop["type"] = jsonSchemaTypeArray
		prop["items"] = map[string]any{"type": jsonSchemaTypeString}
	case "intSlice":
		prop["type"] = jsonSchemaTypeArray
		prop["items"] = map[string]any{"type": jsonSchemaTypeInteger}
	case "duration":
		prop["type"] = jsonSchemaTypeString
		prop["description"] = flag.Usage + " (format: 1h30m, 5m, 30s)"
	default:
		// Default to string for unknown types
		prop["type"] = jsonSchemaTypeString
	}

	// Add default value if present
	if flag.DefValue != "" && flag.DefValue != defaultValueFalse &&
		flag.DefValue != defaultValueEmptyArray {
		prop["default"] = flag.DefValue
	}

	return prop
}

// buildEnumProperty builds a JSON schema property for enum-valued flags.
// Returns nil if the enum has no valid values.
func buildEnumProperty(ev enumValuer, flag *pflag.Flag) map[string]any {
	validValues := ev.ValidValues()
	if len(validValues) == 0 {
		return nil
	}

	prop := map[string]any{
		"type": jsonSchemaTypeString,
		"enum": validValues,
		"description": fmt.Sprintf(
			"%s (valid options: %s)",
			flag.Usage,
			strings.Join(validValues, ", "),
		),
	}

	// Add default value if available
	if d, ok := flag.Value.(defaulter); ok {
		if def := d.Default(); def != nil {
			prop["default"] = fmt.Sprintf("%v", def)
		}
	} else if flag.DefValue != "" {
		prop["default"] = flag.DefValue
	}

	return prop
}

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
	baseName := strings.ReplaceAll(cmdPath, " ", "_")

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
		relativeKey := subCmd.Name()
		if prefix != "" {
			relativeKey = prefix + "_" + subCmd.Name()
		}

		// Determine if this command requires write permission
		// Use subcommand's explicit permission if set, otherwise inherit from parent
		requiresWrite := parentRequiresWrite
		if subCmd.Annotations != nil && subCmd.Annotations[AnnotationPermission] != "" {
			requiresWrite = subCmd.Annotations[AnnotationPermission] == permissionWrite
		}

		subCmdPath := subCmd.CommandPath()
		subCmdParts := strings.Fields(subCmdPath)
		flags := extractFlags(subCmd)

		subcommandDef := &SubcommandDef{
			Name:         relativeKey,
			Description:  subCmd.Short,
			CommandParts: subCmdParts,
			Flags:        flags,
		}

		// If this subcommand has its own children, check if it's also runnable
		if len(subCmd.Commands()) > 0 {
			// Include runnable parent commands (have RunE and non-help flags)
			if isRunnableCommand(subCmd) {
				if requiresWrite {
					(*writeSubcommands)[relativeKey] = subcommandDef
				} else {
					(*readSubcommands)[relativeKey] = subcommandDef
				}
			}
			// Recursively collect nested subcommands with updated prefix
			collectSubcommandsWithPrefix(
				subCmd,
				readSubcommands,
				writeSubcommands,
				relativeKey,
				requiresWrite,
			)
		} else {
			// Leaf command - add it to the appropriate map
			if requiresWrite {
				(*writeSubcommands)[relativeKey] = subcommandDef
			} else {
				(*readSubcommands)[relativeKey] = subcommandDef
			}
		}
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

	if cmd.Long != "" && cmd.Long != cmd.Short {
		description = description + "\n\n" + cmd.Long
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

// extractFlags extracts flag metadata from a command.
func extractFlags(cmd *cobra.Command) map[string]*FlagDef {
	flags := make(map[string]*FlagDef)

	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == flagTypeHelp {
			return
		}

		flagType := mapFlagTypeToJSONType(flag.Value.Type())
		required := flag.DefValue == "" && flag.Value.Type() != flagTypeBool

		flags[flag.Name] = &FlagDef{
			Name:        flag.Name,
			Type:        flagType,
			Description: flag.Usage,
			Required:    required,
			Default:     flag.DefValue,
			// AppliesToSubcommands will be populated during schema building
		}
	})

	return flags
}

// buildConsolidatedParameterSchema builds a dynamic JSON schema for consolidated tools.
func buildConsolidatedParameterSchema(
	subcommandParam string,
	subcommands map[string]*SubcommandDef,
) map[string]any {
	properties := make(map[string]any)
	required := []string{subcommandParam}

	// Add subcommand parameter as enum
	subcommandNames := make([]string, 0, len(subcommands))
	subcommandDescriptions := make([]string, 0, len(subcommands))

	for name, def := range subcommands {
		subcommandNames = append(subcommandNames, name)
		subcommandDescriptions = append(subcommandDescriptions, name+": "+def.Description)
	}

	properties[subcommandParam] = map[string]any{
		"type": jsonSchemaTypeString,
		"enum": subcommandNames,
		"description": "The subcommand to execute. Options:\n" + strings.Join(
			subcommandDescriptions,
			"\n",
		),
	}

	// Merge all flags from all subcommands
	allFlags := make(map[string]*FlagDef)

	for subCmdName, subCmd := range subcommands {
		for flagName, flagDef := range subCmd.Flags {
			if existing, exists := allFlags[flagName]; exists {
				// Flag exists in multiple subcommands - update AppliesToSubcommands
				existing.AppliesToSubcommands = append(existing.AppliesToSubcommands, subCmdName)
			} else {
				// New flag - clone it and track which subcommand it applies to
				newFlagDef := &FlagDef{
					Name:                 flagDef.Name,
					Type:                 flagDef.Type,
					Description:          flagDef.Description,
					Required:             flagDef.Required,
					Default:              flagDef.Default,
					AppliesToSubcommands: []string{subCmdName},
				}
				allFlags[flagName] = newFlagDef
			}
		}
	}

	// Add all flags to properties with conditional annotations
	for flagName, flagDef := range allFlags {
		prop := map[string]any{
			"type": flagDef.Type,
		}

		// Build description with conditional applicability
		description := flagDef.Description
		if len(flagDef.AppliesToSubcommands) < len(subcommands) {
			// Flag doesn't apply to all subcommands - add annotation
			description = fmt.Sprintf(
				"%s (applies to: %s)",
				description,
				strings.Join(flagDef.AppliesToSubcommands, ", "),
			)
		}

		prop["description"] = description

		// Add default if present
		if flagDef.Default != "" && flagDef.Default != defaultValueFalse &&
			flagDef.Default != defaultValueEmptyArray {
			prop["default"] = flagDef.Default
		}

		properties[flagName] = prop

		// Note: We don't add flags to required array since they're conditional
		// The executor will validate based on which subcommand is selected
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}

	return schema
}

// mapFlagTypeToJSONType converts pflag types to JSON schema types.
func mapFlagTypeToJSONType(flagType string) string {
	switch flagType {
	case flagTypeBool:
		return jsonSchemaTypeBoolean
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return jsonSchemaTypeInteger
	case "float32", "float64":
		return jsonSchemaTypeNumber
	case "stringSlice", "stringArray", "intSlice":
		return jsonSchemaTypeArray
	default:
		return jsonSchemaTypeString
	}
}
