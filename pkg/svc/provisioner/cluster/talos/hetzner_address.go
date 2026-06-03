package talosprovisioner

import (
	"fmt"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// hetznerNodeTalosAddress returns the address KSail uses to reach a Hetzner node's
// Talos API (and to derive cluster endpoints). It prefers the node's public IPv4;
// when the node is IPv4-less — e.g. a worker or control-plane provisioned with
// EnableIPv4=false — it falls back to the node's private-network IP.
//
// KSail attaches each node to exactly one private network, so the first PrivateNet
// entry carrying an IP is the cluster network. Reaching an IPv4-less node over its
// private IP requires KSail itself to have private-network reachability (run from
// inside the network, a VPN, or a bastion); see the Hetzner provider documentation.
//
// It returns ErrNodeNoReachableAddress when the node has neither a public IPv4 nor a
// private-network IP.
func hetznerNodeTalosAddress(server *hcloud.Server) (string, error) {
	if server == nil {
		return "", ErrNodeNoReachableAddress
	}

	if !server.PublicNet.IPv4.IsUnspecified() {
		return server.PublicNet.IPv4.IP.String(), nil
	}

	for _, privateNet := range server.PrivateNet {
		if privateNet.IP != nil {
			return privateNet.IP.String(), nil
		}
	}

	return "", fmt.Errorf("%w: %s", ErrNodeNoReachableAddress, server.Name)
}

// diagnoseUnreachableNode enriches a Talos-API connection/timeout failure with
// actionable guidance when the node is IPv4-less. KSail reaches such a node only over
// the private network, so a failure here almost always means KSail has no route into
// the Hetzner private network, or the node has no egress to finish booting — neither
// of which is visible from the cluster config, so it can only be diagnosed at runtime.
// For nodes that have a public IPv4 (the default), the original error is returned
// unchanged. A nil error is passed through.
func diagnoseUnreachableNode(server *hcloud.Server, err error) error {
	if err == nil || server == nil || !server.PublicNet.IPv4.IsUnspecified() {
		return err
	}

	return fmt.Errorf(
		"%w: node %s is IPv4-less and was reached over its private network; ensure ksail "+
			"runs with a route into the Hetzner private network (from inside the network, a "+
			"VPN, or a bastion) and that the node has egress (a NAT gateway or working IPv6): %w",
		ErrPrivateNetworkUnreachable, server.Name, err,
	)
}
