package toolgen

import (
	"errors"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
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
	helpFlagName = "help"
	flagTypeBool = "bool"
)

// Default value strings to skip when adding defaults to schema.
const (
	defaultValueFalse      = "false"
	defaultValueEmptyArray = "[]"
)

// Permission annotation value.
const permissionWrite = "write"

// Sentinel errors for tool execution.
var (
	// ErrMissingSubcommandParam indicates the subcommand parameter is missing or invalid.
	ErrMissingSubcommandParam = errors.New("missing or invalid subcommand parameter")

	// ErrInvalidSubcommand indicates an unknown subcommand was provided.
	ErrInvalidSubcommand = errors.New("invalid subcommand")

	// ErrArgsNotArray indicates the args parameter is not an array.
	ErrArgsNotArray = errors.New("args parameter must be an array")
)
