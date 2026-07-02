package k3shetzner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// Create provisions a K3s cluster on Hetzner Cloud. It runs the shared Hetzner
// create flow ([hetznerbase.Base.RunCreate]) — guard against an existing cluster,
// reject multi-node topologies, ensure the shared infrastructure, and compose the
// bring-up plan. Only the node token (generateNodeToken) and the plan composition
// (composePlan) are k3s-specific; they are handed to the shared flow. Deriving the
// live server specs still needs boot-image resolution and bootstrap-material
// threading (devantler-tech/ksail#5726), so composePlan stops at the
// live-bring-up boundary ([ErrLiveBringUpNotImplemented]); the K3s × Hetzner
// combination is unselectable until the validation flip (#5514), so this path is
// gated either way.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	err := p.RunCreate(ctx, name, p.composePlan, generateNodeToken)
	if err != nil {
		return fmt.Errorf("provision K3s × Hetzner cluster: %w", err)
	}

	return nil
}

// composePlan composes the K3s per-node cloud-init user_data toward the shared
// create flow's bring-up plan ([hetznerbase.Base.RunCreate]). The single-
// control-plane path carries no join server URL and no SSH authorized key yet;
// deriving the composed user_data into live server specs — boot-image
// resolution, the bootstrap keypair, and the pinned host keys — is tracked by
// devantler-tech/ksail#5726, so the composition stops at the live-bring-up
// boundary.
func (p *Provisioner) composePlan(
	clusterName, token string,
	_ hetznerbase.ResolvedInfra,
) (hetznerbase.BringUpPlan, error) {
	nodes, err := p.buildNodeUserData(clusterName, token, "", nil)
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
