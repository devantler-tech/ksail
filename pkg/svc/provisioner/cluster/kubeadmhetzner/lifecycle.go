package kubeadmhetzner

import (
	"context"
	"fmt"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// remoteKubeconfigPath is where kubeadm writes the admin kubeconfig on the
// cluster-initialising control plane; the bring-up engine waits for and reads
// this file after first boot.
const remoteKubeconfigPath = "/etc/kubernetes/admin.conf"

// Create provisions a kubeadm cluster on Hetzner Cloud. It runs the shared Hetzner
// create flow ([hetznerbase.Base.RunCreate]) — guard against an existing cluster,
// reject multi-node topologies, ensure the shared infrastructure, compose the
// bring-up plan, and run the live bring-up to a merged kubeconfig. Only the node
// token (generateNodeToken) and the plan composition (composePlan) are
// kubeadm-specific; they are handed to the shared flow.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	err := p.RunCreate(ctx, name, p.composePlan, generateNodeToken)
	if err != nil {
		return fmt.Errorf("provision Vanilla × Hetzner cluster: %w", err)
	}

	return nil
}

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

// composePlan composes the kubeadm per-node cloud-init user_data into the shared
// create flow's bring-up plan ([hetznerbase.Base.RunCreate]): it mints the
// per-cluster bootstrap material ([hetznerbase.GenerateBootstrapMaterial] — the
// bootstrap client keypair plus the pinned host identity), threads it into every
// node's cloud-init document, derives the live server specs
// ([hetznerbase.DeriveServerSpecs]), and returns the complete plan the live
// bring-up runs from.
func (p *Provisioner) composePlan(
	clusterName, token string,
	infra hetznerbase.ResolvedInfra,
) (hetznerbase.BringUpPlan, error) {
	plan, err := hetznerbase.ComposePlan(
		clusterName, p.Opts, infra, remoteKubeconfigPath,
		func(material hetznerbase.BootstrapMaterial) ([]hetznerbase.NodeSpec, error) {
			nodes, err := p.buildNodes(
				clusterName, token,
				[]string{material.AuthorizedKey}, material.HostKeys,
			)
			if err != nil {
				return nil, err
			}

			specs := make([]hetznerbase.NodeSpec, len(nodes))
			for i, node := range nodes {
				specs[i] = hetznerbase.NodeSpec{
					Index:    node.Index,
					NodeType: nodeType(node.Role),
					UserData: node.UserData,
					Labels:   node.Labels,
				}
			}

			return specs, nil
		},
	)
	if err != nil {
		return hetznerbase.BringUpPlan{}, fmt.Errorf("compose kubeadm bring-up plan: %w", err)
	}

	return plan, nil
}
