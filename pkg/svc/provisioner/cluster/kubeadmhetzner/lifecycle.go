package kubeadmhetzner

import (
	"context"
	"fmt"

	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
)

// Create provisions a kubeadm cluster on Hetzner Cloud. It runs the shared Hetzner
// create flow ([hetznerbase.Base.RunCreate]) — guard against an existing cluster,
// reject multi-node topologies, ensure the shared infrastructure, and compose the
// per-node cloud-init user_data — stopping at the live-bring-up boundary
// ([ErrLiveBringUpNotImplemented], devantler-tech/ksail#5515). Only the node token
// (generateNodeToken) and the user_data composition (composeNodes) are
// kubeadm-specific; they are handed to the shared flow. The Vanilla × Hetzner
// combination is unselectable until the validation flip (#5514), so this path is
// gated.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	err := p.RunCreate(ctx, name, p.composeNodes, generateNodeToken)
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
// repository track every node installs the kube* components from.
func (p *Provisioner) buildNodes(clusterName, token string) ([]NodeUserData, error) {
	nodes, err := BuildNodeUserData(Input{
		ClusterName: clusterName,
		Plan: kubeadmbootstrap.PlanInput{
			Token:             token,
			KubernetesVersion: p.kubernetesVersion,
			ControlPlaneCount: p.ControlPlanes,
			AgentCount:        p.Agents,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("compose node user_data: %w", err)
	}

	return nodes, nil
}

// composeNodes composes the kubeadm per-node cloud-init user_data and returns the
// node count, adapting buildNodes to the shared create flow's composeNodes callback
// ([hetznerbase.Base.RunCreate]).
func (p *Provisioner) composeNodes(clusterName, token string) (int, error) {
	nodes, err := p.buildNodes(clusterName, token)
	if err != nil {
		return 0, err
	}

	return len(nodes), nil
}
