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

// ExportedDescriptorForTest exposes exportedDescriptor for white-box testing.
var ExportedDescriptorForTest = exportedDescriptor

// RequireSamePlatformForTest exposes requireSamePlatform for white-box testing.
var RequireSamePlatformForTest = requireSamePlatform

// PublishArchiveForTest exposes publishArchive with an injectable rename.
var PublishArchiveForTest = publishArchive

// CleanupExportSourcesForTest exposes cleanup of a real export staging directory.
var CleanupExportSourcesForTest = cleanupExportSources

// ArchivePartialPlatformImagesForTest reads an archive and returns the images its export notice
// names, with the platforms the archive carries for them.
func ArchivePartialPlatformImagesForTest(path string) ([]string, []string, error) {
	coverage, err := readArchivePlatformCoverage(path)
	if err != nil {
		return nil, nil, err
	}

	names, carried := partialPlatformImages(coverage)

	return names, carried, nil
}
