//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package image

// IsHelperContainerForTest exposes isHelperContainer for white-box testing.
var IsHelperContainerForTest = isHelperContainer
