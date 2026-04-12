package tenant

// ExportGetResources exposes getResources for use in external test packages.
func ExportGetResources(raw map[string]any) []string {
	return getResources(raw)
}

// ExportResolveKustomizationPath exposes ResolveKustomizationPath for testing.
func ExportResolveKustomizationPath(
	outputDir, explicit string,
) (string, error) {
	return ResolveKustomizationPath(outputDir, explicit)
}

//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var (
	ExportAddResource = addResource
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	ExportRemoveResource         = removeResource
	ExportHasDuplicateNamespaces = hasDuplicateNamespaces
	ExportIsValidType            = isValidType
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	ExportSafeRelPath = safeRelPath

//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
)
