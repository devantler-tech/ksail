package kubeadmhetzner

import (
	"context"
	"fmt"

	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// Create provisions a kubeadm cluster on Hetzner Cloud. It runs the shared Hetzner
// create flow ([hetznerbase.Base.RunCreate]) — guard against an existing cluster,
// reject multi-node topologies, ensure the shared infrastructure, and compose the
// bring-up plan. Only the node token (generateNodeToken) and the plan composition
// (composePlan) are kubeadm-specific; they are handed to the shared flow. Deriving
// the live server specs still needs boot-image resolution and bootstrap-material
// threading (devantler-tech/ksail#5726), so composePlan stops at the
// live-bring-up boundary ([ErrLiveBringUpNotImplemented]); the Vanilla × Hetzner
// combination is unselectable until the validation flip (#5514), so this path is
// gated either way.
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

// composePlan composes the kubeadm per-node cloud-init user_data toward the
// shared create flow's bring-up plan ([hetznerbase.Base.RunCreate]). Deriving
// the composed user_data into live server specs — boot-image resolution, the
// bootstrap keypair, and the pinned host keys — is tracked by
// devantler-tech/ksail#5726, so the composition stops at the live-bring-up
// boundary.
func (p *Provisioner) composePlan(
	clusterName, token string,
	_ hetznerbase.ResolvedInfra,
) (hetznerbase.BringUpPlan, error) {
	nodes, err := p.buildNodes(clusterName, token)
	if err != nil {
		return hetznerbase.BringUpPlan{}, err
	}

	_, _ = fmt.Fprintf(
		p.LogWriter,
		"Prepared cloud-init bootstrap for %d node(s); deriving live server "+
			"specs is tracked by #5726\n",
		len(nodes),
	)

	return hetznerbase.BringUpPlan{}, hetznerbase.ErrLiveBringUpNotImplemented
}
