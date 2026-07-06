package k3shetzner

import (
	"net"

	k3sbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/k3s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// k3sAPIPort is the port a k3s server serves its API (and thus the registration
// endpoint agents dial) on — the standard Kubernetes secure port.
const k3sAPIPort = "6443"

// staticCapabilityChecks assert at compile time that *Provisioner implements the
// optional shared-create-flow capabilities: [hetznerbase.MultiNodeComposer], so a
// K3s topology with agents routes to the two-phase bring-up, and
// [hetznerbase.HAControlPlaneComposer], so a multi-control-plane topology routes
// there too instead of being rejected — K3s's embedded etcd needs no manual
// certificate distribution (each additional server joins the cluster via the
// shared token and its own bootstrap), so the same two-phase compose that plans
// the agents already plans the additional control planes.
var (
	_ hetznerbase.MultiNodeComposer      = (*Provisioner)(nil)
	_ hetznerbase.HAControlPlaneComposer = (*Provisioner)(nil)
)

// SupportsHAControlPlanes marks the K3s strategy as able to compose a
// multi-control-plane topology, satisfying [hetznerbase.HAControlPlaneComposer]:
// [Provisioner.ComposeJoiningNodes] already plans the full topology, so the
// additional control-plane servers (RoleServer, joining the init server's
// embedded etcd via `--server`) are composed alongside the agents. It performs
// no work.
func (p *Provisioner) SupportsHAControlPlanes() {}

// ControlPlaneJoinCompletePath returns the sentinel an additional K3s control
// plane writes once it has joined the embedded etcd and its local API server is
// ready, satisfying [hetznerbase.HAControlPlaneComposer]: the shared flow polls
// it over SSH to serialise control-plane joins, because concurrent joins race
// etcd member addition. The joining server's first boot publishes it only behind
// a readiness gate (see [k3sbootstrap.ServerJoinCompleteSentinelPath]) — the k3s
// install command returns before the etcd join settles, so it can never stand in
// for the sentinel.
func (p *Provisioner) ControlPlaneJoinCompletePath() string {
	return k3sbootstrap.ServerJoinCompleteSentinelPath
}

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
