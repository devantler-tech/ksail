package k3shetzner

import (
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// remoteKubeconfigPath is where k3s writes the admin kubeconfig on the
// cluster-initialising server; the bring-up engine waits for and reads this
// file after first boot.
const remoteKubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

// The K3s create flow is [hetznerbase.Base.Create], inherited by embedding
// *Base; NewProvisioner registers this Provisioner as the base's
// [hetznerbase.CreateStrategy] so the shared flow reaches the k3s-specific
// pieces below.

// ComposeNodes threads the minted bootstrap material into the K3s per-node
// cloud-init user_data and projects it onto the shared [hetznerbase.NodeSpec]
// the bring-up plan derives server specs from. The single-control-plane path
// carries no join server URL (multi-node is rejected before composition).
func (p *Provisioner) ComposeNodes(
	clusterName, token string,
	material hetznerbase.BootstrapMaterial,
) ([]hetznerbase.NodeSpec, error) {
	nodes, err := p.buildNodeUserData(
		clusterName, token, "",
		[]string{material.AuthorizedKey}, material.HostKeys,
	)
	if err != nil {
		return nil, err
	}

	return hetznerbase.NodeSpecsFrom(nodes, func(node nodeUserData) hetznerbase.NodeSpec {
		return hetznerbase.NodeSpec{
			Index:    node.index,
			NodeType: nodeType(node.role),
			UserData: node.userData,
			Labels:   node.labels,
		}
	}), nil
}

// RemoteKubeconfigPath reports where k3s writes the admin kubeconfig, satisfying
// [hetznerbase.CreateStrategy].
func (p *Provisioner) RemoteKubeconfigPath() string { return remoteKubeconfigPath }

// DistroLabel labels the K3s × Hetzner distribution for the create flow's error
// context, satisfying [hetznerbase.CreateStrategy].
func (p *Provisioner) DistroLabel() string { return "K3s × Hetzner" }

// GenerateToken produces the cluster's shared k3s node token, satisfying
// [hetznerbase.CreateStrategy].
func (p *Provisioner) GenerateToken() (string, error) { return generateNodeToken() }
