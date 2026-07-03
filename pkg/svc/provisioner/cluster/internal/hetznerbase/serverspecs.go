package hetznerbase

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
)

// DefaultImageName is the stock OS image every cloud-init-bootstrapped node
// boots: the k3s and kubeadm distributions install onto a stock Ubuntu LTS
// rather than a custom snapshot or ISO. Hetzner resolves the name to the
// architecture-matching image server-side at creation
// ([hetzner.CreateServerOpts.ImageName]), so no client-side image lookup is
// needed.
const DefaultImageName = "ubuntu-24.04"

// NodeSpec pairs a planned node's identity with the cloud-init user_data that
// bootstraps it — the distribution-agnostic per-node shape both the k3s and
// kubeadm provisioners produce before spec derivation places the nodes into
// the cluster's resolved infrastructure.
type NodeSpec struct {
	// Index is the node's zero-based bootstrap position (the
	// cluster-initialising control plane is 0).
	Index int
	// NodeType is the Hetzner node-type label value
	// ([hetzner.NodeTypeControlPlane] or [hetzner.NodeTypeWorker]).
	NodeType string
	// UserData is the cloud-init document delivered as the server's user_data.
	UserData string
	// Labels is the Hetzner label set applied to the server.
	Labels map[string]string
}

// NodeSpecsFrom maps a distro's per-node build output to the shared
// []NodeSpec the bring-up plan derives server specs from, applying toSpec to
// each node in order. It exists so each provisioner's composeNodes callback
// need not re-write the make-and-loop boilerplate — only the per-node field
// projection (toSpec), which differs by the distro's node type, lives at the
// call site.
func NodeSpecsFrom[Node any](nodes []Node, toSpec func(Node) NodeSpec) []NodeSpec {
	specs := make([]NodeSpec, len(nodes))
	for index, node := range nodes {
		specs[index] = toSpec(node)
	}

	return specs
}

// DeriveServerSpecs turns the per-node cloud-init user_data a provisioner
// composed into the ordered [hetzner.CreateServerOpts] fed to the Hetzner
// server-creation API — the composition step between "what to run on each
// node" and "which server runs it", shared by the k3s and kubeadm
// provisioners. For every node it derives the validated server name
// ([hetzner.NodeName], sharing the identity encoded in the node's labels),
// selects the configured per-role server type, boots [DefaultImageName], and
// places the server into the resolved infrastructure.
//
// The index carried by each [NodeSpec] is reused verbatim for the server name
// and matches the node's ksail.node.index label, so a node's name and labels
// always agree. DeriveServerSpecs is pure — no I/O, no network — and never
// returns a partial result: a name that exceeds the DNS-1123 label limit fails
// the whole derivation rather than provisioning a mis-named subset.
func DeriveServerSpecs(
	clusterName string,
	nodes []NodeSpec,
	opts v1alpha1.OptionsHetzner,
	infra ResolvedInfra,
) ([]hetzner.CreateServerOpts, error) {
	specs := make([]hetzner.CreateServerOpts, 0, len(nodes))

	for _, node := range nodes {
		name, err := hetzner.NodeName(clusterName, node.NodeType, node.Index)
		if err != nil {
			return nil, fmt.Errorf("derive server name for node %d: %w", node.Index, err)
		}

		specs = append(specs, hetzner.CreateServerOpts{
			Name:             name,
			ServerType:       nodeServerType(opts, node.NodeType),
			ImageName:        DefaultImageName,
			Location:         opts.Location,
			Labels:           node.Labels,
			UserData:         node.UserData,
			NetworkID:        infra.NetworkID,
			PlacementGroupID: infra.PlacementGroupID,
			SSHKeyID:         infra.SSHKeyID,
			FirewallIDs:      firewallIDs(infra.FirewallID),
		})
	}

	return specs, nil
}

// nodeServerType selects the configured Hetzner server type for a node's role.
// The config layer applies the defaults (see [v1alpha1.OptionsHetzner]), so the
// options arrive resolved here.
func nodeServerType(opts v1alpha1.OptionsHetzner, nodeType string) string {
	if nodeType == hetzner.NodeTypeWorker {
		return opts.WorkerServerType
	}

	return opts.ControlPlaneServerType
}

// firewallIDs wraps a resolved firewall ID as the single-element slice
// [hetzner.CreateServerOpts] expects, or nil when no firewall (a zero ID) is set.
func firewallIDs(firewallID int64) []int64 {
	if firewallID == 0 {
		return nil
	}

	return []int64{firewallID}
}
