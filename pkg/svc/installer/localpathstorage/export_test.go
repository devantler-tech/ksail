package localpathstorageinstaller

// SetManifestURLForTest overrides the upstream manifest location so unit tests can
// serve the manifest from a local test server instead of reaching the network.
func SetManifestURLForTest(installer *Installer, url string) {
	installer.manifestURL = url
}

// ManifestURLForTest exposes the configured manifest location so a test can assert
// the production default still points at the pinned upstream manifest.
func ManifestURLForTest(installer *Installer) string {
	return installer.manifestURL
}

// LocalPathProvisionerVersionForTest exposes the version pinned in the embedded
// Dockerfile, so a test can assert the manifest URL tracks it across dependency
// bumps instead of hard-coding a version that Dependabot will move.
func LocalPathProvisionerVersionForTest() string {
	return localPathProvisionerVersion()
}
