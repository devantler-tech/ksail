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
