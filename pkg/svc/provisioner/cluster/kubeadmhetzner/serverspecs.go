package kubeadmhetzner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
)

// Infra carries the resolved Hetzner infrastructure the server specs are placed
// into. The provisioner fills it after ensuring the network, firewall, placement
// group and SSH key exist and after resolving the base OS image and server type;
// [DeriveServerSpecs] performs no I/O and treats these as given.
//
// This increment provisions a homogeneous cluster: every node shares the same
// server type, location and base image. Per-role sizing (a larger control plane, a
// cheaper worker type) is an additive later increment.
type Infra struct {
	// ServerType is the Hetzner server type every node is created as (e.g. "cx22").
	ServerType string
	// Location is the Hetzner location every node is created in (e.g. "fsn1").
	Location string
	// ImageID is the base OS image every node boots (the provisioner resolves an
	// image such as Ubuntu to its ID). kubeadm installs onto a stock OS image, so —
	// unlike the Talos path's ISO — a kubeadm server always boots an ImageID, never
	// an ISO.
	ImageID int64
	// NetworkID is the private network every node joins.
	NetworkID int64
	// FirewallID is the firewall applied to every node. A zero value attaches no
	// firewall.
	FirewallID int64
	// PlacementGroupID is the placement group every node is created in.
	PlacementGroupID int64
	// SSHKeyID is the SSH key installed on every node.
	SSHKeyID int64
	// EnableIPv4 controls the servers' public IPv4 (nil = the Hetzner default, a
	// public IPv4 is assigned). See [hetzner.CreateServerOpts].
	EnableIPv4 *bool
	// EnableIPv6 controls the servers' public IPv6 (nil = the Hetzner default, a
	// public IPv6 is assigned). See [hetzner.CreateServerOpts].
	EnableIPv6 *bool
}

// DeriveServerSpecs turns the per-node cloud-init user_data produced by
// [BuildNodeUserData] into the ordered [hetzner.CreateServerOpts] the provisioner
// feeds to the Hetzner server-creation API — the composition step between "what to
// run on each node" and "which server runs it". For every node it derives the
// validated server name ([hetzner.NodeName], sharing the identity encoded in the
// node's labels), attaches the node's cloud-init document and Hetzner labels, and
// places the server into the resolved infrastructure.
//
// The index carried by each [NodeUserData] (the zero-based bootstrap position, 0
// being the cluster-initialising control plane) is reused verbatim for the server
// name and matches the node's ksail.node.index label, so a node's name and labels
// always agree. DeriveServerSpecs is pure — no I/O, no network — and never returns
// a partial result: a name that exceeds the DNS-1123 label limit fails the whole
// derivation rather than provisioning a mis-named subset.
func DeriveServerSpecs(
	clusterName string,
	nodes []NodeUserData,
	infra Infra,
) ([]hetzner.CreateServerOpts, error) {
	specs := make([]hetzner.CreateServerOpts, 0, len(nodes))

	for _, node := range nodes {
		name, err := hetzner.NodeName(clusterName, nodeType(node.Role), node.Index)
		if err != nil {
			return nil, fmt.Errorf("derive server name for node %d: %w", node.Index, err)
		}

		specs = append(specs, hetzner.CreateServerOpts{
			Name:             name,
			ServerType:       infra.ServerType,
			ImageID:          infra.ImageID,
			Location:         infra.Location,
			Labels:           node.Labels,
			UserData:         node.UserData,
			NetworkID:        infra.NetworkID,
			PlacementGroupID: infra.PlacementGroupID,
			SSHKeyID:         infra.SSHKeyID,
			FirewallIDs:      firewallIDs(infra.FirewallID),
			EnableIPv4:       infra.EnableIPv4,
			EnableIPv6:       infra.EnableIPv6,
		})
	}

	return specs, nil
}

// firewallIDs wraps a resolved firewall ID as the single-element slice
// [hetzner.CreateServerOpts] expects, or nil when no firewall (a zero ID) is set.
func firewallIDs(firewallID int64) []int64 {
	if firewallID == 0 {
		return nil
	}

	return []int64{firewallID}
}
