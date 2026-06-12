package annotations

const (
	// AnnotationExclude is a command annotation to exclude it from tool generation.
	// Set to "true" to exclude a command and all its subcommands.
	AnnotationExclude = "ai.toolgen.exclude"

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
