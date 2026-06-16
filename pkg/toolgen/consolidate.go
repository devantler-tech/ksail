package toolgen

import (
	"reflect"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/spf13/cobra"
)

// acceptsPositionalArgs returns true if the command accepts positional arguments.
// A command accepts args if:
//   - cmd.Args is nil (Cobra default: no validation, accepts arbitrary args)
//   - cmd.Args is set to a validator other than cobra.NoArgs
//
// It returns false only when cmd.Args is cobra.NoArgs (explicitly rejects all args).
func acceptsPositionalArgs(cmd *cobra.Command) bool {
	if cmd.Args == nil {
		return true
	}

	return reflect.ValueOf(cmd.Args).Pointer() !=
		reflect.ValueOf(cobra.NoArgs).Pointer()
}

// isExcludedFromTools reports whether a command must be dropped from the
// generated tool surface because it carries either the explicit exclude
// annotation or the interactive marker (a TUI/$EDITOR/TTY-picker command that an
// AI client cannot drive). It is enforced both in the unconsolidated walk
// (generator.go) and inside consolidated parents (walkSubcommands) so the policy
// cannot drift between the two code paths.
func isExcludedFromTools(cmd *cobra.Command) bool {
	if cmd.Annotations == nil {
		return false
	}

	return cmd.Annotations[annotations.AnnotationExclude] == annotationValueTrue ||
		cmd.Annotations[annotations.AnnotationInteractive] == annotationValueTrue
}

// shouldConsolidate checks if a command should consolidate its subcommands.
func shouldConsolidate(cmd *cobra.Command) bool {
	if cmd.Annotations == nil {
		return false
	}

	_, hasAnnotation := cmd.Annotations[annotations.AnnotationConsolidate]

	return hasAnnotation && len(cmd.Commands()) > 0
}

// commandToPermissionSplitTools splits a parent command into read and write tools based on permission.
// It flattens all nested subcommands recursively and groups them by permission.
// If the parent has an explicit permission, all subcommands inherit it and create a single tool.
func commandToPermissionSplitTools(cmd *cobra.Command, excludeFlags []string) []ToolDefinition {
	// Get the subcommand parameter name from annotation
	subcommandParam := cmd.Annotations[annotations.AnnotationConsolidate]

	// Build base name and path
	cmdPath := cmd.CommandPath()
	strippedPath := stripRootCommand(cmdPath)
	baseName := strings.ReplaceAll(strippedPath, " ", "_")

	// Check if parent has explicit permission - if so, don't split
	parentHasPermission := cmd.Annotations != nil &&
		cmd.Annotations[annotations.AnnotationPermission] != ""
	parentRequiresWrite := cmd.Annotations != nil &&
		cmd.Annotations[annotations.AnnotationPermission] == permissionWrite

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
			excludeFlags,
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
			excludeFlags,
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
			excludeFlags,
		)
		tools = append(tools, writeTool)
	}

	return tools
}

// collectAllSubcommands collects all subcommands recursively without regard to permission.
// Uses relative path from parent as map key to avoid naming collisions.
func collectAllSubcommands(
	parent *cobra.Command,
	subcommands *map[string]*SubcommandDef,
) {
	walkSubcommands(parent, "", false, func(def *SubcommandDef, key string, _ bool) {
		(*subcommands)[key] = def
	})
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
	walkSubcommands(
		parent,
		"",
		parentRequiresWrite,
		func(def *SubcommandDef, key string, requiresWrite bool) {
			addToSubcommandMap(def, key, requiresWrite, readSubcommands, writeSubcommands)
		},
	)
}

// walkSubcommands recursively walks visible subcommands of cmd, building prefixed
// relative keys and routing each collected SubcommandDef through sink.
// Leaf commands are always collected; parents are collected only when runnable
// (per isRunnableCommand) and are emitted before their children are visited.
// Each subcommand's resolved write permission is passed to sink and inherited
// by its children.
func walkSubcommands(
	cmd *cobra.Command,
	prefix string,
	parentRequiresWrite bool,
	sink func(def *SubcommandDef, key string, requiresWrite bool),
) {
	for _, subCmd := range cmd.Commands() {
		// Skip hidden subcommands, and commands explicitly excluded or marked
		// interactive (full-screen TUI / $EDITOR / TTY picker). The
		// unconsolidated walk honors AnnotationExclude in generator.go, but
		// consolidation returns early before that walk runs, so the consolidation
		// collector must enforce the same policy here — otherwise an annotated
		// interactive command (e.g. cluster connect) still leaks into the
		// consolidated tool's subcommand enum.
		if subCmd.Hidden || isExcludedFromTools(subCmd) {
			continue
		}

		// Build the relative key: prefix_name or just name if no prefix
		relativeKey := buildRelativeKey(prefix, subCmd.Name())

		// Determine if this command requires write permission
		requiresWrite := determineWritePermission(subCmd, parentRequiresWrite)

		hasChildren := len(subCmd.Commands()) > 0

		// Collect leaf commands and runnable parent commands
		if !hasChildren || isRunnableCommand(subCmd) {
			sink(buildSubcommandDef(subCmd, relativeKey), relativeKey, requiresWrite)
		}

		// Recursively collect nested subcommands with updated prefix
		if hasChildren {
			walkSubcommands(subCmd, relativeKey, requiresWrite, sink)
		}
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
	if cmd.Annotations != nil && cmd.Annotations[annotations.AnnotationPermission] != "" {
		return cmd.Annotations[annotations.AnnotationPermission] == permissionWrite
	}

	return parentRequiresWrite
}

// buildSubcommandDef creates a SubcommandDef from a command.
// All flags (including those in ExcludeFlags) are stored so that
// handleConsolidatedTool can forward them at runtime.
func buildSubcommandDef(
	cmd *cobra.Command,
	relativeKey string,
) *SubcommandDef {
	return &SubcommandDef{
		Name:         relativeKey,
		Description:  cmd.Short,
		CommandParts: strings.Fields(cmd.CommandPath()),
		Flags:        extractFlags(cmd),
		AcceptsArgs:  acceptsPositionalArgs(cmd),
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
// excludeFlags lists flags to omit from the generated JSON schema; they still work at runtime.
func buildConsolidatedTool(
	toolName string,
	cmd *cobra.Command,
	subcommandParam string,
	subcommands map[string]*SubcommandDef,
	requiresPermission bool,
	excludeFlags []string,
) ToolDefinition {
	// Build description
	description := cmd.Short
	if cmd.Annotations != nil && cmd.Annotations[annotations.AnnotationDescription] != "" {
		description = cmd.Annotations[annotations.AnnotationDescription]
	}

	// Build dynamic parameter schema
	parameters := buildConsolidatedParameterSchema(subcommandParam, subcommands, excludeFlags)

	cmdPath := cmd.CommandPath()
	cmdParts := strings.Fields(cmdPath)

	return ToolDefinition{
		Name:               toolName,
		Title:              buildToolTitle(toolName),
		Description:        description,
		Parameters:         parameters,
		CommandPath:        cmdPath,
		CommandParts:       cmdParts,
		RequiresPermission: requiresPermission,
		Annotations:        buildAnnotationHints(requiresPermission),
		IsConsolidated:     true,
		SubcommandParam:    subcommandParam,
		Subcommands:        subcommands,
	}
}
