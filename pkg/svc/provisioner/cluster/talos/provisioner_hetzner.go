package talosprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// ensureHetznerInfra creates the network, firewall, placement group, and retrieves
// the SSH key needed for Hetzner cluster provisioning.
func (p *Provisioner) ensureHetznerInfra(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName string,
) (HetznerInfra, error) {
	_, _ = fmt.Fprintf(p.logWriter, "Creating infrastructure resources...\n")

	network, err := hzProvider.EnsureNetwork(ctx, clusterName, p.hetznerOpts.NetworkCIDR)
	if err != nil {
		return HetznerInfra{}, fmt.Errorf("failed to create network: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Network %s created\n", network.Name)

	firewall, err := hzProvider.EnsureFirewall(ctx, clusterName)
	if err != nil {
		return HetznerInfra{}, fmt.Errorf("failed to create firewall: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Firewall %s created\n", firewall.Name)

	placementGroup, err := hzProvider.EnsurePlacementGroup(
		ctx,
		clusterName,
		p.hetznerOpts.PlacementGroupStrategy.String(),
		p.hetznerOpts.PlacementGroup,
	)
	if err != nil {
		return HetznerInfra{}, fmt.Errorf("failed to create placement group: %w", err)
	}

	var placementGroupID int64

	if placementGroup != nil {
		placementGroupID = placementGroup.ID
		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Placement group %s created\n", placementGroup.Name)
	} else {
		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Placement group disabled (strategy: None)\n")
	}

	// Get SSH key if configured
	var sshKeyID int64

	if p.hetznerOpts.SSHKeyName != "" {
		sshKey, keyErr := hzProvider.GetSSHKey(ctx, p.hetznerOpts.SSHKeyName)
		if keyErr != nil {
			return HetznerInfra{}, fmt.Errorf("failed to get SSH key: %w", keyErr)
		}

		if sshKey != nil {
			sshKeyID = sshKey.ID
		}
	}

	return HetznerInfra{
		NetworkID:        network.ID,
		FirewallID:       firewall.ID,
		PlacementGroupID: placementGroupID,
		SSHKeyID:         sshKeyID,
	}, nil
}

// createHetznerNodeGroups creates both control plane and worker node groups.
func (p *Provisioner) createHetznerNodeGroups(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	infra HetznerInfra,
	clusterName string,
) ([]*hcloud.Server, []*hcloud.Server, error) {
	controlPlaneServers, err := p.createHetznerNodes(ctx, hzProvider, infra, HetznerNodeGroupOpts{
		ClusterName: clusterName,
		Role:        RoleControlPlane,
		Count:       p.options.ControlPlaneNodes,
		ServerType:  p.hetznerOpts.ControlPlaneServerType,
		ISOID:       p.talosOpts.ISO,
		Location:    p.hetznerOpts.Location,
	})
	if err != nil {
		return nil, nil, err
	}

	workerServers, err := p.createHetznerNodes(ctx, hzProvider, infra, HetznerNodeGroupOpts{
		ClusterName: clusterName,
		Role:        RoleWorker,
		Count:       p.options.WorkerNodes,
		ServerType:  p.hetznerOpts.WorkerServerType,
		ISOID:       p.talosOpts.ISO,
		Location:    p.hetznerOpts.Location,
	})
	if err != nil {
		return nil, nil, err
	}

	return controlPlaneServers, workerServers, nil
}

// updateConfigsWithEndpoint regenerates Talos configs with the correct endpoint IP.
func (p *Provisioner) updateConfigsWithEndpoint(
	controlPlaneServers []*hcloud.Server,
) error {
	// Regenerate configs with the first control-plane node's public IP as the endpoint.
	// This is necessary because:
	// 1. The original configs were generated with internal network IPs
	// 2. Hetzner nodes are accessed via their public IPs
	// 3. The endpoint IP is embedded in certificates and must match
	firstCPIP := controlPlaneServers[0].PublicNet.IPv4.IP.String()

	_, _ = fmt.Fprintf(p.logWriter, "Regenerating configs with endpoint IP %s...\n", firstCPIP)

	updatedConfigs, err := p.talosConfigs.WithEndpoint(firstCPIP)
	if err != nil {
		return fmt.Errorf("failed to regenerate configs with endpoint: %w", err)
	}

	// Update the stored configs
	p.talosConfigs = updatedConfigs

	return nil
}

// prepareAndApplyConfigs prepares config bundle and applies configuration to all nodes.
func (p *Provisioner) prepareAndApplyConfigs(
	ctx context.Context,
	clusterName string,
	controlPlaneServers, workerServers, allServers []*hcloud.Server,
) error {
	configBundle := p.talosConfigs.Bundle()

	// Wait for Talos API to be reachable on all nodes (maintenance mode)
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for Talos API on %d nodes...\n", len(allServers))

	err := p.waitForHetznerTalosAPI(ctx, allServers)
	if err != nil {
		return fmt.Errorf("failed waiting for Talos API: %w", err)
	}

	// Apply machine configuration to all nodes
	_, _ = fmt.Fprintf(p.logWriter, "Applying machine configuration to nodes...\n")

	return p.applyHetznerConfigs(
		ctx,
		clusterName,
		controlPlaneServers,
		workerServers,
		configBundle,
	)
}

// bootstrapAndFinalize bootstraps etcd, saves configs, and waits for cluster readiness.
func (p *Provisioner) bootstrapAndFinalize(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName string,
	controlPlaneServers, workerServers, allServers []*hcloud.Server,
) error {
	// Detach ISOs and reboot
	_, _ = fmt.Fprintf(p.logWriter, "Detaching ISOs and rebooting nodes...\n")

	err := p.detachISOsAndReboot(ctx, hzProvider, allServers)
	if err != nil {
		return fmt.Errorf("failed to detach ISOs: %w", err)
	}

	// Bootstrap etcd cluster
	_, _ = fmt.Fprintf(p.logWriter, "Bootstrapping etcd cluster...\n")

	configBundle := p.talosConfigs.Bundle()

	err = p.bootstrapHetznerCluster(ctx, controlPlaneServers[0], configBundle)
	if err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	// Save talosconfig
	if p.options.TalosconfigPath != "" {
		err = p.saveTalosconfig(configBundle)
		if err != nil {
			return fmt.Errorf("failed to save talosconfig: %w", err)
		}
	}

	// Save kubeconfig and wait for readiness
	if p.options.KubeconfigPath != "" {
		_, _ = fmt.Fprintf(p.logWriter, "Fetching and saving kubeconfig...\n")

		err = p.saveHetznerKubeconfig(ctx, controlPlaneServers[0], configBundle)
		if err != nil {
			return fmt.Errorf("failed to save kubeconfig: %w", err)
		}

		// Wait for cluster to be fully ready before reporting success
		_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

		err = p.waitForHetznerClusterReady(
			ctx,
			clusterName,
			controlPlaneServers,
			workerServers,
			configBundle,
		)
		if err != nil {
			return fmt.Errorf("cluster readiness check failed: %w", err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")
	}

	return nil
}

// createHetznerCluster creates a Talos cluster on Hetzner Cloud infrastructure.
func (p *Provisioner) createHetznerCluster(ctx context.Context, clusterName string) error {
	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Creating Talos cluster %q on Hetzner Cloud...\n", clusterName)

	// Check if cluster already exists
	exists, err := hzProvider.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	// Create infrastructure resources
	infra, err := p.ensureHetznerInfra(ctx, hzProvider, clusterName)
	if err != nil {
		return err
	}

	// Create node groups
	controlPlaneServers, workerServers, err := p.createHetznerNodeGroups(
		ctx, hzProvider, infra, clusterName,
	)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "\nInfrastructure created. Bootstrapping Talos cluster...\n")

	// Update configs with correct endpoint
	err = p.updateConfigsWithEndpoint(controlPlaneServers)
	if err != nil {
		return err
	}

	// Build combined server list (used for config application, ISO detachment, etc.)
	allServers := make([]*hcloud.Server, 0, len(controlPlaneServers)+len(workerServers))
	allServers = append(allServers, controlPlaneServers...)
	allServers = append(allServers, workerServers...)

	// Prepare and apply configs
	err = p.prepareAndApplyConfigs(ctx, clusterName, controlPlaneServers, workerServers, allServers)
	if err != nil {
		return fmt.Errorf("failed while waiting for Talos API or applying machine configuration: %w", err)
	}

	// Bootstrap, save configs, and wait for cluster readiness
	err = p.bootstrapAndFinalize(
		ctx, hzProvider, clusterName,
		controlPlaneServers, workerServers, allServers,
	)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"\nSuccessfully created Talos cluster %q on Hetzner Cloud\n",
		clusterName,
	)

	return nil
}

func (p *Provisioner) deleteHetznerCluster(ctx context.Context, clusterName string) error {
	hetznerProv, err := p.hetznerProvider()
	if err != nil {
		return err
	}

	// Check if cluster exists
	exists, err := hetznerProv.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, clusterName)
	}

	// Delete all nodes and infrastructure
	_, _ = fmt.Fprintf(p.logWriter, "Deleting Talos cluster %q on Hetzner...\n", clusterName)

	err = hetznerProv.DeleteNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete Hetzner nodes: %w", err)
	}

	// Clean up kubeconfig
	if p.options.KubeconfigPath != "" {
		cleanupErr := p.cleanupKubeconfig(clusterName)
		if cleanupErr != nil {
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to clean up kubeconfig: %v\n",
				cleanupErr,
			)
		}
	}

	// Clean up talosconfig
	if p.options.TalosconfigPath != "" {
		cleanupErr := p.cleanupTalosconfig(clusterName)
		if cleanupErr != nil {
			_, _ = fmt.Fprintf(
				p.logWriter,
				"Warning: failed to clean up talosconfig: %v\n",
				cleanupErr,
			)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "Successfully deleted Talos cluster %q\n", clusterName)

	return nil
}
