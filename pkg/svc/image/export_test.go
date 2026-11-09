//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package image

// IsHelperContainerForTest exposes isHelperContainer for white-box testing.
var IsHelperContainerForTest = isHelperContainer

// Every test in this package's binary sees a fixed export archive name, so
// node-side command expectations stay exact. The default generator is exposed
// below and tested on its own.
//
//nolint:gochecknoinits // The seam must be in place before parallel tests start.
func init() {
	newExportArchiveName = func() string { return "ksail-images-export.tar" }
}

// DefaultExportArchiveNameForTest exposes the production archive name generator.
var DefaultExportArchiveNameForTest = defaultExportArchiveName

// RemoveNodeArchiveForTest exposes removeNodeArchive for white-box testing.
var RemoveNodeArchiveForTest = (*Exporter).removeNodeArchive
