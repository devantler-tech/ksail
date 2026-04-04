package tenant

// ExportGetResources exposes getResources for use in external test packages.
func ExportGetResources(raw map[string]any) []string {
	return getResources(raw)
}

// ExportResolveKustomizationPath exposes resolveKustomizationPath for testing.
func ExportResolveKustomizationPath(
	outputDir, explicit string,
) (string, error) {
	return resolveKustomizationPath(outputDir, explicit)
}
