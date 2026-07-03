package kubeadmhetzner

import (
	"fmt"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// remoteKubeconfigPath is where kubeadm writes the admin kubeconfig on the
// cluster-initialising control plane; the bring-up engine waits for and reads
// this file after first boot.
const remoteKubeconfigPath = "/etc/kubernetes/admin.conf"

// The kubeadm create flow is [hetznerbase.Base.Create], inherited by embedding
// *Base; NewProvisioner registers this Provisioner as the base's
// [hetznerbase.CreateStrategy] so the shared flow reaches the kubeadm-specific
// pieces below.

// buildNodes composes the ordered per-node cloud-init user_data for the cluster's
// topology via [BuildNodeUserData]. In the current increment it is only reached for
// a single control-plane, no-agent cluster (multi-node returns
// [ErrMultiNodeNotImplemented] before this is called), so the plan carries no
// APIServerEndpoint. The cluster-wide Kubernetes version selects the package
// repository track every node installs the kube* components from;
// sshAuthorizedKeys and hostKeys deliver the bring-up's bootstrap material
// (the bootstrap public key and the pinned host identity) into every node.
func (p *Provisioner) buildNodes(
	clusterName, token string,
	sshAuthorizedKeys []string,
	hostKeys *cloudinitbootstrap.HostKeys,
) ([]NodeUserData, error) {
	nodes, err := BuildNodeUserData(Input{
		ClusterName: clusterName,
		Plan: kubeadmbootstrap.PlanInput{
			Token:             token,
			KubernetesVersion: p.kubernetesVersion,
			ControlPlaneCount: p.ControlPlanes,
			AgentCount:        p.Agents,
		},
		SSHAuthorizedKeys: sshAuthorizedKeys,
		HostKeys:          hostKeys,
	})
	if err != nil {
		return nil, fmt.Errorf("compose node user_data: %w", err)
	}

	return nodes, nil
}

// ComposeNodes threads the minted bootstrap material into the kubeadm per-node
// cloud-init user_data and projects it onto the shared [hetznerbase.NodeSpec]
// the bring-up plan derives server specs from.
func (p *Provisioner) ComposeNodes(
	clusterName, token string,
	material hetznerbase.BootstrapMaterial,
) ([]hetznerbase.NodeSpec, error) {
	nodes, err := p.buildNodes(
		clusterName, token,
		[]string{material.AuthorizedKey}, material.HostKeys,
	)
	if err != nil {
		return nil, err
	}

	return hetznerbase.NodeSpecsFrom(nodes, func(node NodeUserData) hetznerbase.NodeSpec {
		return hetznerbase.NodeSpec{
			Index:    node.Index,
			NodeType: nodeType(node.Role),
			UserData: node.UserData,
			Labels:   node.Labels,
		}
	}), nil
}

// RemoteKubeconfigPath reports where kubeadm writes the admin kubeconfig,
// satisfying [hetznerbase.CreateStrategy].
func (p *Provisioner) RemoteKubeconfigPath() string { return remoteKubeconfigPath }

// DistroLabel labels the Vanilla × Hetzner distribution for the create flow's
// error context, satisfying [hetznerbase.CreateStrategy].
func (p *Provisioner) DistroLabel() string { return "Vanilla × Hetzner" }

// GenerateToken produces the cluster's shared kubeadm join token, satisfying
// [hetznerbase.CreateStrategy].
func (p *Provisioner) GenerateToken() (string, error) { return generateNodeToken() }
