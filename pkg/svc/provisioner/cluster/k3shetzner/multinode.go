package k3shetzner

import (
	"net"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// k3sAPIPort is the port a k3s server serves its API (and thus the registration
// endpoint agents dial) on — the standard Kubernetes secure port.
const k3sAPIPort = "6443"

// staticMultiNodeComposerCheck asserts at compile time that *Provisioner
// implements the optional [hetznerbase.MultiNodeComposer] capability, so the
// shared create flow routes a K3s topology with agents to the two-phase bring-up.
var _ hetznerbase.MultiNodeComposer = (*Provisioner)(nil)

// ComposeInitNode composes the single cluster-initialising K3s server (bootstrap
// index 0), which carries no join server URL, satisfying
// [hetznerbase.MultiNodeComposer].
func (p *Provisioner) ComposeInitNode(
	clusterName, token string,
	material hetznerbase.BootstrapMaterial,
) (hetznerbase.NodeSpec, error) {
	nodes, err := p.buildNodeUserData(
		clusterName, token, "",
		1, 0,
		[]string{material.AuthorizedKey}, material.HostKeys,
	)
	if err != nil {
		return hetznerbase.NodeSpec{}, err
	}

	return specsFromNodes(nodes)[0], nil
}

// ComposeJoiningNodes composes the K3s agents that register against the init
// server reachable at joinAddress (its private-network IPv4), satisfying
// [hetznerbase.MultiNodeComposer]. It plans the full topology so the agents keep
// their global bootstrap indices, threads the derived server URL into their
// install commands, and returns only the joining nodes (the init node at index 0
// is already up).
func (p *Provisioner) ComposeJoiningNodes(
	clusterName, token string,
	joinAddress net.IP,
	material hetznerbase.BootstrapMaterial,
) ([]hetznerbase.NodeSpec, error) {
	serverURL := "https://" + net.JoinHostPort(joinAddress.String(), k3sAPIPort)

	nodes, err := p.buildNodeUserData(
		clusterName, token, serverURL,
		p.ControlPlanes, p.Agents,
		[]string{material.AuthorizedKey}, material.HostKeys,
	)
	if err != nil {
		return nil, err
	}

	return specsFromNodes(nodes)[1:], nil
}
