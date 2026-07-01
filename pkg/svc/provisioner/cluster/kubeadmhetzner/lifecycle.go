package kubeadmhetzner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
)

// Create provisions a kubeadm cluster on Hetzner Cloud. In the current increment it
// guards against an existing cluster, ensures the shared infrastructure (network,
// firewall, placement group, SSH key), and composes the per-node cloud-init
// user_data, then stops at the live-bring-up boundary: creating the servers (which
// needs boot-image resolution), the runtime join sequencing, and retrieving the
// kubeconfig land with the Hetzner system-test lane (see [ErrLiveBringUpNotImplemented],
// devantler-tech/ksail#5515). Multi-node topologies return
// [ErrMultiNodeNotImplemented]. The Vanilla × Hetzner combination is unselectable
// until the validation flip (#5514), so this path is gated.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	clusterName := p.resolveName(name)

	exists, err := p.infra.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("check existing nodes: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	if p.controlPlanes > 1 || p.agents > 0 {
		return ErrMultiNodeNotImplemented
	}

	err = p.ensureInfrastructure(ctx, clusterName)
	if err != nil {
		return err
	}

	token, err := generateNodeToken()
	if err != nil {
		return err
	}

	nodes, err := p.buildNodes(clusterName, token)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"Prepared cloud-init bootstrap for %d node(s); server creation and "+
			"kubeconfig retrieval are tracked by #5515\n",
		len(nodes),
	)

	return ErrLiveBringUpNotImplemented
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
			ControlPlaneCount: p.controlPlanes,
			AgentCount:        p.agents,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("compose node user_data: %w", err)
	}

	return nodes, nil
}

// ensureInfrastructure creates (or reuses) the cluster's shared Hetzner
// resources: the private network, the firewall, the placement group, and the SSH
// key when one is configured.
func (p *Provisioner) ensureInfrastructure(ctx context.Context, clusterName string) error {
	cidr := p.opts.NetworkCIDR
	if cidr == "" {
		cidr = v1alpha1.DefaultHetznerNetworkCIDR
	}

	_, err := p.infra.EnsureNetwork(ctx, clusterName, cidr)
	if err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}

	_, err = p.infra.EnsureFirewall(ctx, clusterName, p.opts.AllowedCIDRs)
	if err != nil {
		return fmt.Errorf("ensure firewall: %w", err)
	}

	_, err = p.infra.EnsurePlacementGroup(
		ctx,
		clusterName,
		p.opts.PlacementGroupStrategy.String(),
		p.opts.PlacementGroup,
	)
	if err != nil {
		return fmt.Errorf("ensure placement group: %w", err)
	}

	if p.opts.SSHKeyName != "" {
		_, err = p.infra.GetSSHKey(ctx, p.opts.SSHKeyName)
		if err != nil {
			return fmt.Errorf("get SSH key: %w", err)
		}
	}

	return nil
}

// Delete removes the cluster's servers. It is a no-op (nil) when the cluster's
// network does not exist, mirroring the k3s × Hetzner delete guard.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	clusterName := p.resolveName(name)

	networkExists, err := p.infra.NetworkExists(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("check network: %w", err)
	}

	if !networkExists {
		return nil
	}

	err = p.infra.DeleteNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("delete nodes: %w", err)
	}

	return nil
}

// Start starts the cluster's servers.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	err := p.infra.StartNodes(ctx, p.resolveName(name))
	if err != nil {
		return fmt.Errorf("start nodes: %w", err)
	}

	return nil
}

// Stop stops the cluster's servers.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	err := p.infra.StopNodes(ctx, p.resolveName(name))
	if err != nil {
		return fmt.Errorf("stop nodes: %w", err)
	}

	return nil
}

// List returns the names of all clusters the Hetzner provider manages.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	clusters, err := p.infra.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}

	return clusters, nil
}

// Exists reports whether servers exist for the named cluster.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	exists, err := p.infra.NodesExist(ctx, p.resolveName(name))
	if err != nil {
		return false, fmt.Errorf("check nodes exist: %w", err)
	}

	return exists, nil
}
