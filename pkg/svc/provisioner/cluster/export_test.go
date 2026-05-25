package clusterprovisioner

// ExportWrapK3kServerArgs exposes wrapK3kServerArgs for testing.
func ExportWrapK3kServerArgs(args []string) []string {
	return wrapK3kServerArgs(args)
}
