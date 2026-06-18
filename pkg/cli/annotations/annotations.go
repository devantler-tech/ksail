package annotations

const (
	// AnnotationExclude is a command annotation to exclude it from tool generation.
	// Set to "true" to exclude a command and all its subcommands.
	AnnotationExclude = "ai.toolgen.exclude"

	// AnnotationInteractive marks a command that requires an interactive terminal
	// (a full-screen TUI, an $EDITOR, or a TTY picker) and therefore cannot be
	// driven by an AI tool client. Set to "true" to drop the command from the
	// generated MCP/chat tool surface — both in the unconsolidated walk and inside
	// consolidated parents (where AnnotationExclude is not honored). Unlike
	// AnnotationExclude, it documents intent: the command is omitted because it is
	// interactive, not because it is a meta/server command.
	AnnotationInteractive = "ai.toolgen.interactive"

	// AnnotationDescription is a command annotation to provide a custom description.
	// If not set, the command's Short description is used.
	AnnotationDescription = "ai.toolgen.description"

	// AnnotationPermission is a command annotation to indicate permission requirements.
	// Set to "write" for commands that modify state and need user confirmation.
	AnnotationPermission = "ai.toolgen.permission"

	// AnnotationConsolidate is a command annotation to consolidate subcommands into a single tool.
	// The value specifies the parameter name for subcommand selection (e.g., "resource_type", "action", "operation").
	AnnotationConsolidate = "ai.toolgen.consolidate"

	// AnnotationConfirmFlag is a FLAG annotation (set via cmd.Flags().SetAnnotation)
	// marking a boolean flag whose only effect is to skip KSail's own interactive
	// confirmation prompt. The chat assistant auto-injects such flags after
	// SDK-native permission approval. Flags with other semantics (e.g. kubectl's
	// destructive --force, or init's force=overwrite) must NOT carry it.
	AnnotationConfirmFlag = "ai.toolgen.confirm-flag"

	// AnnotationValueTrue is the value that enables boolean annotations such as
	// AnnotationExclude and AnnotationConfirmFlag.
	AnnotationValueTrue = "true"
)
