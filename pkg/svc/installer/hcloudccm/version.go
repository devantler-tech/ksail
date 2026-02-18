package hcloudccminstaller

// chartVersion returns the pinned Hetzner Cloud Controller Manager chart version.
// The chart version diverges from the CCM image version, so it cannot be
// tracked via a Dockerfile image tag. This constant must be updated manually.
func chartVersion() string {
	return "1.29.2"
}
