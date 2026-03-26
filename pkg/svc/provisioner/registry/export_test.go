package registry

// ExportCalculateRegistryIPs exports calculateRegistryIPs for testing.
func ExportCalculateRegistryIPs(networkCIDR string, count int) []string {
	return calculateRegistryIPs(networkCIDR, count)
}

// ExportStaticIPAt exports staticIPAt for testing.
func ExportStaticIPAt(ips []string, idx int) string {
	return staticIPAt(ips, idx)
}
