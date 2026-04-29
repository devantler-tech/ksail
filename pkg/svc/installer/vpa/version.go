package vpainstaller

// chartVersion returns the pinned VPA chart version.
// The chart version (4.x) diverges from the app version (1.x), so it cannot be
// tracked via a Dockerfile image tag. This constant must be updated manually.
func chartVersion() string {
	return "4.11.0"
}
