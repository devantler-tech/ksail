package toolgen

import (
	"errors"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
)

// Re-export annotation constants from pkg/cli/annotations for backward compatibility.
// These constants are now defined in the annotations package to allow CLI commands
// to use them without depending on the toolgen package.
//
// IMPORTANT: The annotations package must never import toolgen to avoid circular dependencies.
const (
	AnnotationExclude     = annotations.AnnotationExclude
	AnnotationDescription = annotations.AnnotationDescription
	AnnotationPermission  = annotations.AnnotationPermission
	AnnotationConsolidate = annotations.AnnotationConsolidate
)

// JSON Schema type constants.
const (
	jsonSchemaTypeString  = "string"
	jsonSchemaTypeBoolean = "boolean"
	jsonSchemaTypeInteger = "integer"
	jsonSchemaTypeNumber  = "number"
	jsonSchemaTypeArray   = "array"
)

// Cobra flag type constants.
const (
	helpFlagName        = "help"
	flagTypeBool        = "bool"
	flagTypeStringSlice = "stringSlice"
	flagTypeStringArray = "stringArray"
	flagTypeIntSlice    = "intSlice"
)

// Default value strings to skip when adding defaults to schema.
const (
	defaultValueFalse      = "false"
	defaultValueEmptyArray = "[]"
)

// Token optimization limits.
const (
	// maxDescriptionLength is the maximum length for parameter descriptions.
	// Verbose kubectl help text is truncated to this limit.
	maxDescriptionLength = 200

	// maxSubcommandDescLength is the maximum length for subcommand descriptions
	// in the consolidated enum property.
	maxSubcommandDescLength = 60

	// appliesToThreshold is the fraction of total subcommands above which
	// the "(applies to: ...)" annotation is omitted. When a flag applies to
	// >= 50% of subcommands, the annotation adds more noise than signal.
	appliesToThreshold = 0.50

	// maxTotalFlagDescriptionLength is the upper bound for a flag description
	// including the "(applies to: ...)" suffix. Prevents the total from
	// exceeding a reasonable size when many subcommands are listed.
	maxTotalFlagDescriptionLength = 400
)

// Parameter key for positional arguments.
const argsKey = "args"

// Annotation value constants.
const (
	annotationValueTrue = "true"
	permissionWrite     = "write"
)

// Sentinel errors for tool execution.
var (
	// ErrMissingSubcommandParam indicates the subcommand parameter is missing or invalid.
	ErrMissingSubcommandParam = errors.New("missing or invalid subcommand parameter")

	// ErrInvalidSubcommand indicates an unknown subcommand was provided.
	ErrInvalidSubcommand = errors.New("invalid subcommand")

	// ErrArgsNotArray indicates the args parameter is not an array.
	ErrArgsNotArray = errors.New("args parameter must be an array")

	// ErrArgsNotAccepted indicates args were provided for a subcommand that rejects them.
	ErrArgsNotAccepted = errors.New("positional args not accepted by subcommand")
)
