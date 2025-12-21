package metadata

import "github.com/devantler-tech/ksail/v5/pkg/io/validator"

// ValidateMetadata validates Kind and APIVersion fields using provided expected values.
func ValidateMetadata(
	kind, apiVersion, expectedKind, expectedAPIVersion string,
	result *validator.ValidationResult,
) {
	// Validate Kind field
	if kind == "" {
		result.AddError(validator.ValidationError{
			Field:         "kind",
			Message:       "kind is required",
			ExpectedValue: expectedKind,
			FixSuggestion: "Set kind to '" + expectedKind + "'",
		})
	} else if kind != expectedKind {
		result.AddError(validator.ValidationError{
			Field:         "kind",
			Message:       "kind does not match expected value",
			CurrentValue:  kind,
			ExpectedValue: expectedKind,
			FixSuggestion: "Set kind to '" + expectedKind + "'",
		})
	}

	// Validate APIVersion field
	if apiVersion == "" {
		result.AddError(validator.ValidationError{
			Field:         "apiVersion",
			Message:       "apiVersion is required",
			ExpectedValue: expectedAPIVersion,
			FixSuggestion: "Set apiVersion to '" + expectedAPIVersion + "'",
		})
	} else if apiVersion != expectedAPIVersion {
		result.AddError(validator.ValidationError{
			Field:         "apiVersion",
			Message:       "apiVersion does not match expected value",
			CurrentValue:  apiVersion,
			ExpectedValue: expectedAPIVersion,
			FixSuggestion: "Set apiVersion to '" + expectedAPIVersion + "'",
		})
	}
}
