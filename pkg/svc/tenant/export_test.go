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

// ExportAddResource exposes addResource for use in external test packages.
var ExportAddResource = addResource

// ExportRemoveResource exposes removeResource for use in external test packages.
var ExportRemoveResource = removeResource

// ExportHasDuplicateNamespaces exposes hasDuplicateNamespaces for use in external test packages.
var ExportHasDuplicateNamespaces = hasDuplicateNamespaces

// ExportIsValidType exposes isValidType for use in external test packages.
var ExportIsValidType = isValidType

// ExportSafeRelPath exposes safeRelPath for use in external test packages.
var ExportSafeRelPath = safeRelPath
