package toolgen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// buildParameterSchema creates a JSON schema from Cobra command flags.
func buildParameterSchema(cmd *cobra.Command) map[string]any {
	properties := make(map[string]any)
	required := []string{}

	// Visit all flags (local and persistent)
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		// Skip help flag
		if flag.Name == helpFlagName {
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
	prop := buildStandardProperty(flag)

	// Add default value if present
	addDefaultValue(prop, flag)

	return prop
}

// buildStandardProperty creates a property map with type information based on flag type.
func buildStandardProperty(flag *pflag.Flag) map[string]any {
	prop := map[string]any{
		"description": flag.Usage,
	}

	switch flag.Value.Type() {
	case flagTypeBool:
		prop["type"] = jsonSchemaTypeBoolean
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		prop["type"] = jsonSchemaTypeInteger
	case "float32", "float64":
		prop["type"] = jsonSchemaTypeNumber
	case flagTypeStringSlice, flagTypeStringArray:
		prop["type"] = jsonSchemaTypeArray
		prop["items"] = map[string]any{"type": jsonSchemaTypeString}
	case flagTypeIntSlice:
		prop["type"] = jsonSchemaTypeArray
		prop["items"] = map[string]any{"type": jsonSchemaTypeInteger}
	case "duration":
		prop["type"] = jsonSchemaTypeString
		prop["description"] = flag.Usage + " (format: 1h30m, 5m, 30s)"
	default:
		prop["type"] = jsonSchemaTypeString
	}

	return prop
}

// addDefaultValue adds a default value to the property if applicable.
func addDefaultValue(prop map[string]any, flag *pflag.Flag) {
	if flag.DefValue != "" && flag.DefValue != defaultValueFalse &&
		flag.DefValue != defaultValueEmptyArray {
		// Get the JSON schema type from the prop map
		flagType, _ := prop["type"].(string)
		prop["default"] = convertDefaultValue(flagType, flag.DefValue)
	}
}

// convertDefaultValue converts a string default value to its proper type based on the JSON schema type.
// This ensures that boolean, integer, and number types have typed default values rather than strings,
// which is required by strict JSON Schema validators like the MCP SDK.
func convertDefaultValue(jsonSchemaType string, defaultStr string) any {
	switch jsonSchemaType {
	case jsonSchemaTypeBoolean:
		// Parse boolean from string ("true" -> true, "false" -> false)
		if defaultStr == "true" {
			return true
		}

		return false
	case jsonSchemaTypeInteger:
		// Parse integer from string
		val, err := strconv.ParseInt(defaultStr, 10, 64)
		if err == nil {
			return val
		}

		return defaultStr // Fallback to string if parsing fails
	case jsonSchemaTypeNumber:
		// Parse float from string
		val, err := strconv.ParseFloat(defaultStr, 64)
		if err == nil {
			return val
		}

		return defaultStr // Fallback to string if parsing fails
	default:
		// For strings, arrays, and other types, return as-is
		return defaultStr
	}
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

// extractFlags extracts flag metadata from a command.
func extractFlags(cmd *cobra.Command) map[string]*FlagDef {
	flags := make(map[string]*FlagDef)

	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == helpFlagName {
			return
		}

		rawFlagType := flag.Value.Type()
		flagType := mapFlagTypeToJSONType(rawFlagType)
		itemsType := mapFlagTypeToItemsType(rawFlagType)
		required := flag.DefValue == "" && rawFlagType != flagTypeBool

		flags[flag.Name] = &FlagDef{
			Name:        flag.Name,
			Type:        flagType,
			ItemsType:   itemsType,
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
	properties[subcommandParam] = buildSubcommandEnumProperty(subcommands)

	// Merge and add all flags from subcommands
	allFlags := mergeSubcommandFlags(subcommands)
	addFlagProperties(properties, allFlags, len(subcommands))

	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// buildSubcommandEnumProperty creates the enum property for subcommand selection.
func buildSubcommandEnumProperty(subcommands map[string]*SubcommandDef) map[string]any {
	names := make([]string, 0, len(subcommands))
	descriptions := make([]string, 0, len(subcommands))

	for name, def := range subcommands {
		names = append(names, name)
		descriptions = append(descriptions, name+": "+def.Description)
	}

	return map[string]any{
		"type":        jsonSchemaTypeString,
		"enum":        names,
		"description": "The subcommand to execute. Options:\n" + strings.Join(descriptions, "\n"),
	}
}

// mergeSubcommandFlags collects all flags from subcommands, tracking which subcommands each applies to.
// When multiple subcommands have the same flag name, the flag definition (type, description, etc.)
// is taken from whichever subcommand is processed last, while AppliesToSubcommands tracks all
// subcommands that use this flag. For consistent behavior, flags with the same name should have
// the same type and description across subcommands.
func mergeSubcommandFlags(subcommands map[string]*SubcommandDef) map[string]*FlagDef {
	allFlags := make(map[string]*FlagDef)

	for subCmdName, subCmd := range subcommands {
		for flagName, flagDef := range subCmd.Flags {
			if existing, exists := allFlags[flagName]; exists {
				existing.AppliesToSubcommands = append(existing.AppliesToSubcommands, subCmdName)
			} else {
				allFlags[flagName] = &FlagDef{
					Name:                 flagDef.Name,
					Type:                 flagDef.Type,
					ItemsType:            flagDef.ItemsType,
					Description:          flagDef.Description,
					Required:             flagDef.Required,
					Default:              flagDef.Default,
					AppliesToSubcommands: []string{subCmdName},
				}
			}
		}
	}

	return allFlags
}

// addFlagProperties adds flag definitions to the properties map with conditional annotations.
func addFlagProperties(
	properties map[string]any,
	allFlags map[string]*FlagDef,
	totalSubcommands int,
) {
	for flagName, flagDef := range allFlags {
		prop := map[string]any{
			"type": flagDef.Type,
		}

		// Add items property for array types (required by JSON Schema)
		if flagDef.ItemsType != "" {
			prop["items"] = map[string]any{"type": flagDef.ItemsType}
		}

		// Build description with conditional applicability
		description := flagDef.Description
		if len(flagDef.AppliesToSubcommands) < totalSubcommands {
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
			// Convert string default to proper type (boolean, integer, number)
			defaultStr, ok := flagDef.Default.(string)
			if ok {
				prop["default"] = convertDefaultValue(flagDef.Type, defaultStr)
			} else {
				// Already typed (shouldn't happen with current code, but handle gracefully)
				prop["default"] = flagDef.Default
			}
		}

		properties[flagName] = prop
	}
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

// mapFlagTypeToItemsType returns the JSON schema type for array items.
// Returns empty string for non-array types.
func mapFlagTypeToItemsType(flagType string) string {
	switch flagType {
	case "stringSlice", "stringArray":
		return jsonSchemaTypeString
	case "intSlice":
		return jsonSchemaTypeInteger
	default:
		return ""
	}
}
