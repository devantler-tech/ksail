package localpathstorageinstaller

import "net/http"

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

// SetManifestServerForTest points the installer at a local test server and sends the
// request over that server's own transport. Sharing http.DefaultTransport would let a
// parallel test's server.Close() drop this test's live connection.
func SetManifestServerForTest(installer *Installer, url string, transport http.RoundTripper) {
	installer.manifestURL = url
	installer.transport = transport
}
