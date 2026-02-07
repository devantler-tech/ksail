package talosprovisioner

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/go-retry/retry"
	"github.com/siderolabs/talos/pkg/cluster/check"
	"github.com/siderolabs/talos/pkg/conditions"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/siderolabs/talos/pkg/provision/access"
)

// ensureHetznerInfra creates the network, firewall, placement group, and retrieves
// the SSH key needed for Hetzner cluster provisioning.
func (p *TalosProvisioner) ensureHetznerInfra(
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

// createHetznerCluster creates a Talos cluster on Hetzner Cloud infrastructure.
//
//nolint:cyclop,funlen // Complex function with sequential steps for cloud provisioning
func (p *TalosProvisioner) createHetznerCluster(ctx context.Context, clusterName string) error {
	hzProvider, ok := p.infraProvider.(*hetzner.Provider)
	if !ok {
		return fmt.Errorf("%w: got %T", ErrHetznerProviderRequired, p.infraProvider)
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

	// Create infrastructure resources (network, firewall, placement group, SSH key)
	infra, err := p.ensureHetznerInfra(ctx, hzProvider, clusterName)
	if err != nil {
		return err
	}

	controlPlaneServers, err := p.createHetznerNodes(ctx, hzProvider, infra, HetznerNodeGroupOpts{
		ClusterName: clusterName,
		Role:        RoleControlPlane,
		Count:       p.options.ControlPlaneNodes,
		ServerType:  p.hetznerOpts.ControlPlaneServerType,
		ISOID:       p.talosOpts.ISO,
		Location:    p.hetznerOpts.Location,
	})
	if err != nil {
		return err
	}

	workerServers, err := p.createHetznerNodes(ctx, hzProvider, infra, HetznerNodeGroupOpts{
		ClusterName: clusterName,
		Role:        "worker",
		Count:       p.options.WorkerNodes,
		ServerType:  p.hetznerOpts.WorkerServerType,
		ISOID:       p.talosOpts.ISO,
		Location:    p.hetznerOpts.Location,
	})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "\nInfrastructure created. Bootstrapping Talos cluster...\n")

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

	// Update the stored configs and get the bundle
	p.talosConfigs = updatedConfigs
	configBundle := updatedConfigs.Bundle()

	// Build list of all node IPs for waiting
	allServers := make([]*hcloud.Server, 0, len(controlPlaneServers)+len(workerServers))
	allServers = append(allServers, controlPlaneServers...)
	allServers = append(allServers, workerServers...)

	// Wait for Talos API to be reachable on all nodes (maintenance mode)
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for Talos API on %d nodes...\n", len(allServers))

	err = p.waitForHetznerTalosAPI(ctx, allServers)
	if err != nil {
		return fmt.Errorf("failed waiting for Talos API: %w", err)
	}

	// Apply machine configuration to all nodes
	_, _ = fmt.Fprintf(p.logWriter, "Applying machine configuration to nodes...\n")

	err = p.applyHetznerConfigs(
		ctx,
		clusterName,
		controlPlaneServers,
		workerServers,
		configBundle,
	)
	if err != nil {
		return fmt.Errorf("failed to apply machine configuration: %w", err)
	}

	// Detach ISOs from all servers so they boot from disk instead of ISO
	_, _ = fmt.Fprintf(p.logWriter, "Detaching ISOs and rebooting nodes...\n")

	err = p.detachISOsAndReboot(ctx, hzProvider, allServers)
	if err != nil {
		return fmt.Errorf("failed to detach ISOs: %w", err)
	}

	// Bootstrap the cluster on the first control-plane node
	_, _ = fmt.Fprintf(p.logWriter, "Bootstrapping etcd cluster...\n")

	err = p.bootstrapHetznerCluster(ctx, controlPlaneServers[0], configBundle)
	if err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	// Save talosconfig
	if p.options.TalosconfigPath != "" {
		saveErr := p.saveTalosconfig(configBundle)
		if saveErr != nil {
			return fmt.Errorf("failed to save talosconfig: %w", saveErr)
		}
	}

	// Save kubeconfig
	if p.options.KubeconfigPath != "" {
		_, _ = fmt.Fprintf(p.logWriter, "Fetching and saving kubeconfig...\n")

		err = p.saveHetznerKubeconfig(ctx, controlPlaneServers[0], configBundle)
		if err != nil {
			return fmt.Errorf("failed to save kubeconfig: %w", err)
		}

		// Wait for cluster to be fully ready before reporting success
		// This uses upstream Talos SDK check.Wait() pattern
		_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

		waitErr := p.waitForHetznerClusterReady(
			ctx,
			clusterName,
			controlPlaneServers,
			workerServers,
			configBundle,
		)
		if waitErr != nil {
			return fmt.Errorf("cluster readiness check failed: %w", waitErr)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"\nSuccessfully created Talos cluster %q on Hetzner Cloud\n",
		clusterName,
	)

	return nil
}

// waitForHetznerTalosAPI waits for the Talos API to be reachable on all Hetzner servers.
// Nodes booted from ISO are in maintenance mode and expose the Talos API on port 50000.
func (p *TalosProvisioner) waitForHetznerTalosAPI(
	ctx context.Context,
	servers []*hcloud.Server,
) error {
	for _, server := range servers {
		serverIP := server.PublicNet.IPv4.IP.String()
		endpoint := fmt.Sprintf("%s:%d", serverIP, talosAPIPort)

		_, _ = fmt.Fprintf(
			p.logWriter,
			"  Waiting for Talos API on %s (%s)...\n",
			server.Name,
			endpoint,
		)

		err := retry.Constant(talosAPIWaitTimeout, retry.WithUnits(retryInterval)).
			RetryWithContext(ctx, func(ctx context.Context) error {
				// Try to establish a TLS connection to verify the Talos API is responding
				// In maintenance mode, we can only verify the connection works - most APIs
				// return "not implemented in maintenance mode" which is expected
				retryClient, connErr := talosclient.New(ctx,
					talosclient.WithEndpoints(serverIP),
					talosclient.WithTLSConfig(&tls.Config{
						InsecureSkipVerify: true, //nolint:gosec // Maintenance mode requires insecure connection
					}),
				)
				if connErr != nil {
					return retry.ExpectedError(connErr)
				}

				defer retryClient.Close() //nolint:errcheck

				// Try to get version - in maintenance mode this may return "not implemented"
				// but that error indicates the API is reachable and responding
				_, versionErr := retryClient.Version(ctx)
				if versionErr != nil {
					// "Unimplemented" means the API is reachable but in maintenance mode
					// This is actually a success - the node is ready for config application
					if strings.Contains(versionErr.Error(), "Unimplemented") {
						return nil
					}

					return retry.ExpectedError(versionErr)
				}

				return nil
			})
		if err != nil {
			return fmt.Errorf("timeout waiting for Talos API on %s: %w", server.Name, err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Talos API reachable on %s\n", server.Name)
	}

	return nil
}

// applyHetznerConfigs applies machine configuration to all Hetzner nodes.
// It uses the insecure Talos client to connect to nodes in maintenance mode.
func (p *TalosProvisioner) applyHetznerConfigs(
	ctx context.Context,
	_ string,
	controlPlaneServers []*hcloud.Server,
	workerServers []*hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	// Get control-plane and worker configs
	cpConfig := configBundle.ControlPlane()
	workerConfig := configBundle.Worker()

	// Apply control-plane config to all control-plane nodes
	for _, server := range controlPlaneServers {
		err := p.applyConfigToNode(ctx, server, cpConfig)
		if err != nil {
			return fmt.Errorf("failed to apply config to %s: %w", server.Name, err)
		}
	}

	// Apply worker config to all worker nodes
	for _, server := range workerServers {
		err := p.applyConfigToNode(ctx, server, workerConfig)
		if err != nil {
			return fmt.Errorf("failed to apply config to %s: %w", server.Name, err)
		}
	}

	return nil
}

// detachISOsAndReboot handles the post-config-apply phase of Hetzner Talos installation.
//
// After ApplyConfiguration, Talos runs the install sequence which:
// 1. Installs Talos to disk (creates STATE, EPHEMERAL partitions)
// 2. Automatically reboots the server
//
// On Hetzner, after reboot with an installed disk, the server typically boots from disk
// even with ISO still attached (disk gets higher boot priority after install).
//
// This function:
// 1. Waits for the installation + automatic reboot to complete
// 2. Waits for servers to become reachable (connection refused during reboot)
// 3. Detaches ISOs for cleanliness (not strictly required but good practice)
//
// Note: We cannot reliably poll STATE partition because the server reboots automatically
// during install, which breaks our insecure TLS connection.
//
//nolint:funlen // Sequential steps for cloud provisioning
func (p *TalosProvisioner) detachISOsAndReboot(
	ctx context.Context,
	hetznerProv *hetzner.Provider,
	servers []*hcloud.Server,
) error {
	_, _ = fmt.Fprintf(
		p.logWriter,
		"  Waiting for installation and automatic reboot to complete...\n",
	)
	_, _ = fmt.Fprintf(
		p.logWriter,
		"  (Talos will install to disk and reboot automatically - this takes 3-5 minutes)\n",
	)

	// Wait for all servers to complete installation and reboot
	// During this time:
	// - Nodes install Talos to disk (1-2 minutes)
	// - Nodes automatically reboot (from install sequence)
	// - Nodes boot from disk and come up with authenticated TLS
	//
	// We detect completion by waiting for a TCP connection to succeed on port 50000
	// (the server will be unreachable during reboot, then come back)
	for _, server := range servers {
		serverIP := server.PublicNet.IPv4.IP.String()

		_, _ = fmt.Fprintf(
			p.logWriter,
			"  Waiting for %s to install, reboot, and become reachable...\n",
			server.Name,
		)

		// Wait for server to become reachable after installation + reboot
		// This waits through the entire install cycle:
		// - Initial "connection refused" during install
		// - Then more "connection refused" during reboot
		// - Finally success when booted from disk
		err := retry.Constant(clusterReadinessTimeout, retry.WithUnits(longRetryInterval)).
			RetryWithContext(ctx, func(ctx context.Context) error {
				// Just check if we can establish a TCP connection
				// We don't care about TLS here, just network reachability
				dialer := &net.Dialer{Timeout: retryInterval}

				conn, dialErr := dialer.DialContext(ctx, "tcp", net.JoinHostPort(serverIP, "50000"))
				if dialErr != nil {
					return retry.ExpectedError(
						fmt.Errorf("waiting for server to become reachable: %w", dialErr),
					)
				}

				_ = conn.Close()

				return nil
			})
		if err != nil {
			return fmt.Errorf(
				"timeout waiting for %s to become reachable after install: %w",
				server.Name,
				err,
			)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ %s is reachable after install\n", server.Name)
	}

	// Now detach ISOs for cleanliness
	// This isn't strictly required (disk has boot priority after install)
	// but it's good practice to clean up
	for _, server := range servers {
		_, _ = fmt.Fprintf(p.logWriter, "  Detaching ISO from %s...\n", server.Name)

		err := hetznerProv.DetachISO(ctx, server)
		if err != nil {
			// Log but don't fail - ISO detachment is not critical
			_, _ = fmt.Fprintf(
				p.logWriter,
				"  Warning: Failed to detach ISO from %s: %v\n",
				server.Name,
				err,
			)
		} else {
			_, _ = fmt.Fprintf(p.logWriter, "  ✓ ISO detached from %s\n", server.Name)
		}
	}

	return nil
}

// applyConfigToNode applies machine configuration to a single Hetzner node.
func (p *TalosProvisioner) applyConfigToNode(
	ctx context.Context,
	server *hcloud.Server,
	config talosconfig.Provider,
) error {
	serverIP := server.PublicNet.IPv4.IP.String()

	_, _ = fmt.Fprintf(p.logWriter, "  Applying config to %s (%s)...\n", server.Name, serverIP)

	// Get config bytes
	cfgBytes, err := config.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Create insecure client for maintenance mode
	talosClient, err := talosclient.New(ctx,
		talosclient.WithEndpoints(serverIP),
		talosclient.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // Maintenance mode requires insecure connection
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	// Apply configuration - the node will install and reboot
	_, err = talosClient.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: cfgBytes,
	})
	if err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  ✓ Config applied to %s (node will install and reboot)\n",
		server.Name,
	)

	return nil
}

// bootstrapHetznerCluster bootstraps the etcd cluster on the first control-plane node.
//
//nolint:funlen // Bootstrap sequence is inherently complex but logically coherent
func (p *TalosProvisioner) bootstrapHetznerCluster(
	ctx context.Context,
	bootstrapNode *hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	nodeIP := bootstrapNode.PublicNet.IPv4.IP.String()

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  Waiting for %s to be ready for bootstrap...\n",
		bootstrapNode.Name,
	)

	// After config is applied, nodes will reboot. We need to wait for the Talos API
	// to come back up with the applied configuration (authenticated mode).
	talosConfig := configBundle.TalosConfig()

	// Wait for the node to come back after installation
	timeout := clusterReadinessTimeout

	err := retry.Constant(timeout, retry.WithUnits(longRetryInterval)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			// Create authenticated client using talosconfig
			retryClient, clientErr := talosclient.New(ctx,
				talosclient.WithEndpoints(nodeIP),
				talosclient.WithConfig(talosConfig),
			)
			if clientErr != nil {
				return retry.ExpectedError(clientErr)
			}

			defer retryClient.Close() //nolint:errcheck

			// Try to get version to verify the node is ready
			_, versionErr := retryClient.Version(ctx)
			if versionErr != nil {
				return retry.ExpectedError(versionErr)
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("timeout waiting for node to be ready after installation: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Node %s is ready\n", bootstrapNode.Name)

	// Create authenticated client for bootstrap
	talosClient, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeIP),
		talosclient.WithConfig(talosConfig),
	)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	_, _ = fmt.Fprintf(p.logWriter, "  Bootstrapping etcd on %s...\n", bootstrapNode.Name)

	// Bootstrap the cluster
	err = retry.Constant(bootstrapTimeout, retry.WithUnits(retryInterval)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			bootstrapErr := talosClient.Bootstrap(ctx, &machineapi.BootstrapRequest{})
			if bootstrapErr != nil {
				// FailedPrecondition means the node isn't ready yet
				if talosclient.StatusCode(bootstrapErr) == grpcFailedPrecondition {
					return retry.ExpectedError(bootstrapErr)
				}

				return fmt.Errorf("bootstrap failed: %w", bootstrapErr)
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Etcd cluster bootstrapped\n")

	// Wait for cluster to be ready
	_, _ = fmt.Fprintf(p.logWriter, "  Waiting for Kubernetes to be ready...\n")

	err = retry.Constant(timeout, retry.WithUnits(longRetryInterval)).
		RetryWithContext(ctx, func(ctx context.Context) error {
			// Try to fetch kubeconfig as an indicator that K8s is ready
			_, kubeconfigErr := talosClient.Kubeconfig(ctx)
			if kubeconfigErr != nil {
				return retry.ExpectedError(kubeconfigErr)
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("timeout waiting for Kubernetes to be ready: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Kubernetes is ready\n")

	return nil
}

// saveHetznerKubeconfig fetches and saves the kubeconfig from a Hetzner control-plane node.
func (p *TalosProvisioner) saveHetznerKubeconfig(
	ctx context.Context,
	controlPlaneNode *hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	nodeIP := controlPlaneNode.PublicNet.IPv4.IP.String()
	talosConfig := configBundle.TalosConfig()

	// Create authenticated client
	talosClient, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeIP),
		talosclient.WithConfig(talosConfig),
	)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	// Fetch kubeconfig
	kubeconfig, err := talosClient.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch kubeconfig: %w", err)
	}

	// The kubeconfig from Talos uses internal IPs. For Hetzner, we need to use the public IP.
	// Rewrite the server endpoint to use the public IP.
	kubeconfig, err = rewriteKubeconfigEndpoint(
		kubeconfig,
		"https://"+net.JoinHostPort(nodeIP, "6443"),
	)
	if err != nil {
		return fmt.Errorf("failed to rewrite kubeconfig endpoint: %w", err)
	}

	// Write kubeconfig to the configured path
	err = p.writeKubeconfig(kubeconfig)
	if err != nil {
		return err
	}

	return nil
}

// deleteHetznerCluster deletes a Talos cluster on Hetzner Cloud infrastructure.
func (p *TalosProvisioner) deleteHetznerCluster(ctx context.Context, clusterName string) error {
	// Type assert to get Hetzner-specific provider
	hetznerProv, ok := p.infraProvider.(*hetzner.Provider)
	if !ok {
		return fmt.Errorf("%w: got %T", ErrHetznerProviderRequired, p.infraProvider)
	}

	// Check if cluster exists
	exists, err := hetznerProv.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererrors.ErrClusterNotFound, clusterName)
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

// createHetznerNodes provisions a set of Hetzner Cloud servers for a specific node role.
// It is a helper used by createHetznerCluster to create control-plane and worker nodes.
//
// Parameters:
//   - ctx: request-scoped context for cancellation and timeouts.
//   - provider: Hetzner infrastructure provider used to create the servers.
//   - infra: shared infrastructure resources (network, firewall, placement group, SSH key).
//   - opts: node group specification (cluster name, role, count, server type, ISO, location).
//
// Returns:
//   - []*hcloud.Server: slice of successfully created servers (empty if count <= 0).
//   - error: non-nil if any server creation fails.
func (p *TalosProvisioner) createHetznerNodes(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	infra HetznerInfra,
	opts HetznerNodeGroupOpts,
) ([]*hcloud.Server, error) {
	if opts.Count <= 0 {
		return []*hcloud.Server{}, nil
	}

	_, _ = fmt.Fprintf(p.logWriter, "Creating %d %s node(s)...\n", opts.Count, opts.Role)

	// Build retry options from Hetzner config
	retryOpts := hetzner.ServerRetryOpts{
		LogWriter: p.logWriter,
	}

	if p.hetznerOpts != nil {
		retryOpts.FallbackLocations = p.hetznerOpts.FallbackLocations
		retryOpts.AllowPlacementFallback = p.hetznerOpts.PlacementGroupFallbackToNone
	}

	servers := make([]*hcloud.Server, 0, opts.Count)
	for nodeIndex := range opts.Count {
		nodeName := fmt.Sprintf("%s-%s-%d", opts.ClusterName, opts.Role, nodeIndex+1)

		server, err := hzProvider.CreateServerWithRetry(ctx, hetzner.CreateServerOpts{
			Name:             nodeName,
			ServerType:       opts.ServerType,
			ISOID:            opts.ISOID,
			Location:         opts.Location,
			Labels:           hetzner.NodeLabels(opts.ClusterName, opts.Role, nodeIndex+1),
			NetworkID:        infra.NetworkID,
			PlacementGroupID: infra.PlacementGroupID,
			SSHKeyID:         infra.SSHKeyID,
			FirewallIDs:      []int64{infra.FirewallID},
		}, retryOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s node %s: %w", opts.Role, nodeName, err)
		}

		servers = append(servers, server)

		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ✓ %s node %s created (IP: %s)\n",
			opts.Role,
			server.Name,
			server.PublicNet.IPv4.IP.String(),
		)
	}

	return servers, nil
}

// newHetznerClusterWithEndpoint constructs a HetznerClusterResult with the
// Kubernetes API endpoint derived from the first control-plane server's public IPv4.
func newHetznerClusterWithEndpoint(
	clusterName string,
	controlPlaneServers []*hcloud.Server,
	workerServers []*hcloud.Server,
) (*HetznerClusterResult, error) {
	kubeEndpoint := "https://" + net.JoinHostPort(
		controlPlaneServers[0].PublicNet.IPv4.IP.String(),
		"6443",
	)

	return NewHetznerClusterResult(clusterName, controlPlaneServers, workerServers, kubeEndpoint)
}

// waitForHetznerClusterReady waits for the Hetzner cluster to be fully ready.
// This uses the upstream Talos SDK's access.NewAdapter() and check.Wait() patterns
// to perform the same readiness checks as Docker-based clusters.
//
// The checks performed depend on whether CNI is disabled:
//   - With CNI: Full checks including node Ready status
//   - Without CNI: PreBootSequence + K8sComponentsReadiness checks only
//
// This ensures the cluster is actually usable when creation returns.
func (p *TalosProvisioner) waitForHetznerClusterReady(
	ctx context.Context,
	clusterName string,
	controlPlaneServers []*hcloud.Server,
	workerServers []*hcloud.Server,
	configBundle *bundle.Bundle,
) error {
	hetznerCluster, err := newHetznerClusterWithEndpoint(
		clusterName,
		controlPlaneServers,
		workerServers,
	)
	if err != nil {
		return fmt.Errorf("failed to create cluster result: %w", err)
	}

	// Get Talos config for authenticated client access
	talosConfig := configBundle.TalosConfig()

	// Create ClusterAccess adapter using upstream SDK pattern
	// This provides the full ClusterInfo interface (ClientProvider, K8sProvider, Info)
	clusterAccess := access.NewAdapter(
		hetznerCluster,
		provision.WithTalosConfig(talosConfig),
	)

	defer clusterAccess.Close() //nolint:errcheck

	// Determine which checks to run based on CNI configuration
	// When CNI is disabled, nodes won't become Ready until CNI is installed
	return p.runHetznerClusterChecks(ctx, clusterAccess)
}

// waitForHetznerClusterReadyAfterStart waits for a Hetzner cluster to be ready after starting.
// This is similar to waitForHetznerClusterReady but loads the TalosConfig from disk
// instead of from the config bundle (which is not available during start operations).
func (p *TalosProvisioner) waitForHetznerClusterReadyAfterStart(
	ctx context.Context,
	clusterName string,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

	// Discover and classify servers by role
	controlPlaneServers, workerServers, err := p.discoverHetznerServers(ctx, clusterName)
	if err != nil {
		return err
	}

	// Build the cluster result from the discovered servers
	hetznerCluster, err := newHetznerClusterWithEndpoint(
		clusterName,
		controlPlaneServers,
		workerServers,
	)
	if err != nil {
		return fmt.Errorf("failed to create cluster result: %w", err)
	}

	// Load TalosConfig from disk (since we don't have the config bundle during start)
	talosConfig, err := clientconfig.Open("")
	if err != nil {
		return fmt.Errorf("failed to load talosconfig: %w", err)
	}

	// Create ClusterAccess adapter using upstream SDK pattern
	clusterAccess := access.NewAdapter(
		hetznerCluster,
		provision.WithTalosConfig(talosConfig),
	)

	defer clusterAccess.Close() //nolint:errcheck

	err = p.runHetznerClusterChecks(ctx, clusterAccess)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")

	return nil
}

// discoverHetznerServers lists nodes for a cluster and classifies them by role,
// returning separate slices of control-plane and worker servers.
func (p *TalosProvisioner) discoverHetznerServers(
	ctx context.Context,
	clusterName string,
) ([]*hcloud.Server, []*hcloud.Server, error) {
	nodes, err := p.infraProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", clustererrors.ErrNoNodesFound, clusterName)
	}

	hetznerProvider, ok := p.infraProvider.(*hetzner.Provider)
	if !ok {
		return nil, nil, clustererrors.ErrNotHetznerProvider
	}

	var controlPlaneServers, workerServers []*hcloud.Server

	for _, node := range nodes {
		server, serverErr := hetznerProvider.GetServerByName(ctx, node.Name)
		if serverErr != nil {
			return nil, nil, fmt.Errorf("failed to get server %s: %w", node.Name, serverErr)
		}

		if server == nil {
			continue
		}

		if node.Role == RoleControlPlane {
			controlPlaneServers = append(controlPlaneServers, server)
		} else {
			workerServers = append(workerServers, server)
		}
	}

	if len(controlPlaneServers) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", clustererrors.ErrNoControlPlaneNodes, clusterName)
	}

	return controlPlaneServers, workerServers, nil
}

// runHetznerClusterChecks runs CNI-aware readiness checks on a Hetzner cluster.
// It selects the appropriate checks based on CNI configuration, logs progress,
// and waits for all checks to pass.
func (p *TalosProvisioner) runHetznerClusterChecks(
	ctx context.Context,
	clusterAccess *access.Adapter,
) error {
	checks := p.clusterReadinessChecks()

	if (p.talosConfigs != nil && p.talosConfigs.IsCNIDisabled()) || p.options.SkipCNIChecks {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  Running pre-boot and K8s component checks (CNI not installed yet)...\n",
		)
	} else {
		_, _ = fmt.Fprintf(p.logWriter, "  Running full cluster readiness checks...\n")
	}

	reporter := &hetznerCheckReporter{writer: p.logWriter}

	checkCtx, cancel := context.WithTimeout(ctx, clusterReadinessTimeout)
	defer cancel()

	err := check.Wait(checkCtx, clusterAccess, checks, reporter)
	if err != nil {
		return fmt.Errorf("cluster readiness checks failed: %w", err)
	}

	return nil
}

// hetznerCheckReporter implements check.Reporter to log check progress.
type hetznerCheckReporter struct {
	writer   io.Writer
	lastLine string
}

func (r *hetznerCheckReporter) Update(condition conditions.Condition) {
	line := fmt.Sprintf("    %s", condition)
	if line != r.lastLine {
		_, _ = fmt.Fprintf(r.writer, "%s\n", line)
		r.lastLine = line
	}
}
