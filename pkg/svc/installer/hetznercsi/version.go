package hetznercsiinstaller

// chartVersion returns the pinned Hetzner CSI chart version.
// The chart version diverges from the CSI driver image version, so it cannot be
// tracked via a Dockerfile image tag. This constant must be updated manually.
func chartVersion() string {
	return "2.18.3"
}
