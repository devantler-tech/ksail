package image

// isHelperContainer reports whether a node role corresponds to a helper container
// (load balancer, tools, registry) rather than an actual Kubernetes node with containerd.
func isHelperContainer(role string) bool {
	switch role {
	case "loadbalancer", // K3d load balancer proxy
		"noRole",   // K3d tools container
		"registry": // K3d registry container
		return true
	default:
		return false
	}
}
