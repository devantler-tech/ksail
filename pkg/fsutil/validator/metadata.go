package validator

// ValidateMetadata validates Kind and APIVersion fields using provided expected values.
//
// This helper function validates Kubernetes-style metadata fields that are common
// across different configuration types (Kind, K3d, Ksail).
func ValidateMetadata(
	kind, apiVersion, expectedKind, expectedAPIVersion string,
	result *ValidationResult,
) {
	// Validate Kind field
	if kind == "" {
		result.AddError(ValidationError{
			Field:         "kind",
			Message:       "kind is required",
			ExpectedValue: expectedKind,
			FixSuggestion: "Set kind to '" + expectedKind + "'",
		})
	} else if kind != expectedKind {
		result.AddError(ValidationError{
			Field:         "kind",
			Message:       "kind does not match expected value",
			CurrentValue:  kind,
			ExpectedValue: expectedKind,
			FixSuggestion: "Set kind to '" + expectedKind + "'",
		})
	}

	// Validate APIVersion field
	if apiVersion == "" {
		result.AddError(ValidationError{
			Field:         "apiVersion",
			Message:       "apiVersion is required",
			ExpectedValue: expectedAPIVersion,
			FixSuggestion: "Set apiVersion to '" + expectedAPIVersion + "'",
		})
	} else if apiVersion != expectedAPIVersion {
		result.AddError(ValidationError{
			Field:         "apiVersion",
			Message:       "apiVersion does not match expected value",
			CurrentValue:  apiVersion,
			ExpectedValue: expectedAPIVersion,
			FixSuggestion: "Set apiVersion to '" + expectedAPIVersion + "'",
		})
	}
}
