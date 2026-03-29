package workload

// Test exports for validate_expansion.go.
// These are only compiled during testing.

var (
	ExportExpandFluxSubstitutions = expandFluxSubstitutions //nolint:gochecknoglobals // test export
	ExportGetSchemaTypeAtPath     = getSchemaTypeAtPath     //nolint:gochecknoglobals // test export
	ExportSchemaURLs              = schemaURLs              //nolint:gochecknoglobals // test export
	ExportSplitAPIVersion         = splitAPIVersion         //nolint:gochecknoglobals // test export
	ExportTypedPlaceholderValue   = typedPlaceholderValue   //nolint:gochecknoglobals // test export
)
