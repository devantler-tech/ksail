package metricsserverinstaller

// chartVersion returns the pinned metrics-server chart version.
// The chart version (3.x) diverges from the app version (0.x), so it cannot be
// tracked via a Dockerfile image tag. This constant must be updated manually.
func chartVersion() string {
	return "3.13.0"
}
