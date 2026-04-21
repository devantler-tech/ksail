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
