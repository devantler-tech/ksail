package talosprovisioner

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/go-retry/retry"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
)

// createHetznerNodes creates a batch of Hetzner servers for a given role (control-plane or worker).
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
func (p *Provisioner) createHetznerNodes(
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

// waitForHetznerTalosAPI waits for the Talos API to be reachable on all Hetzner servers.
// Nodes booted from ISO are in maintenance mode and expose the Talos API on port 50000.
func (p *Provisioner) waitForHetznerTalosAPI(
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
func (p *Provisioner) applyHetznerConfigs(
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
func (p *Provisioner) detachISOsAndReboot(
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
func (p *Provisioner) applyConfigToNode(
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
	insecureClient, err := talosclient.New(ctx,
		talosclient.WithEndpoints(serverIP),
		talosclient.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // Required for maintenance mode
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create Talos client: %w", err)
	}

	defer insecureClient.Close() //nolint:errcheck

	// Apply configuration
	_, err = insecureClient.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: cfgBytes,
	})
	if err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Config applied to %s\n", server.Name)

	return nil
}
