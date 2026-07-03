package k3shetzner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// remoteKubeconfigPath is where k3s writes the admin kubeconfig on the
// cluster-initialising server; the bring-up engine waits for and reads this
// file after first boot.
const remoteKubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

// Create provisions a K3s cluster on Hetzner Cloud. It runs the shared Hetzner
// create flow ([hetznerbase.Base.RunCreate]) — guard against an existing cluster,
// reject multi-node topologies, ensure the shared infrastructure, compose the
// bring-up plan, and run the live bring-up to a merged kubeconfig. Only the node
// token (generateNodeToken) and the plan composition (composePlan) are
// k3s-specific; they are handed to the shared flow.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	err := p.RunCreate(ctx, name, p.composePlan, generateNodeToken)
	if err != nil {
		return fmt.Errorf("provision K3s × Hetzner cluster: %w", err)
	}

	return nil
}

// composePlan composes the K3s per-node cloud-init user_data into the shared
// create flow's bring-up plan ([hetznerbase.Base.RunCreate]): it mints the
// per-cluster bootstrap material ([hetznerbase.GenerateBootstrapMaterial] — the
// bootstrap client keypair plus the pinned host identity), threads it into every
// node's cloud-init document, derives the live server specs
// ([hetznerbase.DeriveServerSpecs]), and returns the complete plan the live
// bring-up runs from. The single-control-plane path carries no join server URL
// (multi-node is rejected before composition).
func (p *Provisioner) composePlan(
	clusterName, token string,
	infra hetznerbase.ResolvedInfra,
) (hetznerbase.BringUpPlan, error) {
	plan, err := hetznerbase.ComposePlan(
		clusterName, p.Opts, infra, remoteKubeconfigPath,
		func(material hetznerbase.BootstrapMaterial) ([]hetznerbase.NodeSpec, error) {
			nodes, err := p.buildNodeUserData(
				clusterName, token, "",
				[]string{material.AuthorizedKey}, material.HostKeys,
			)
			if err != nil {
				return nil, err
			}

			specs := make([]hetznerbase.NodeSpec, len(nodes))
			for i, node := range nodes {
				specs[i] = hetznerbase.NodeSpec{
					Index:    node.index,
					NodeType: nodeType(node.role),
					UserData: node.userData,
					Labels:   node.labels,
				}
			}

			return specs, nil
		},
	)
	if err != nil {
		return hetznerbase.BringUpPlan{}, fmt.Errorf("compose K3s bring-up plan: %w", err)
	}

	return plan, nil
}
