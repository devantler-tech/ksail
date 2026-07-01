package k3shetzner

import (
	"context"
	"fmt"
)

// Create provisions a K3s cluster on Hetzner Cloud. It runs the shared Hetzner
// create flow ([hetznerbase.Base.RunCreate]) — guard against an existing cluster,
// reject multi-node topologies, ensure the shared infrastructure, and compose the
// per-node cloud-init user_data — stopping at the live-bring-up boundary
// ([ErrLiveBringUpNotImplemented], devantler-tech/ksail#5515). Only the node token
// (generateNodeToken) and the user_data composition (composeNodes) are k3s-specific;
// they are handed to the shared flow. The K3s × Hetzner combination is unselectable
// until the validation flip (#5514), so this path is gated.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	err := p.RunCreate(ctx, name, p.composeNodes, generateNodeToken)
	if err != nil {
		return fmt.Errorf("provision K3s × Hetzner cluster: %w", err)
	}

	return nil
}
